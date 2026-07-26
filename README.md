# Bien gérer ses transactions en base de données — le code

Dépôt compagnon de l'article [Bien gérer ses transactions en base de données](https://clevertechware.fr/blog/posts/2026/gerer-les-transactions-en-base-de-donnees).

L'article défend six thèses en SQL. Ce dépôt les met en Go, et surtout : **chaque thèse a un test qui la prouve**, pas un test qui la répète. Le test central chronomètre un verrou tenu pendant un appel réseau. Il mesure ~1,5 s d'attente sur le chemin fautif, ~2 ms sur le chemin corrigé — même travail, même durée totale, même résultat en base.

## Démarrage

```bash
make db-up        # PostgreSQL 18
make run-remote   # le prestataire externe lent (port 9090)
make run-server   # l'API (port 8080)
```

```bash
make test-unit          # rapide, sans Docker
make test-integration   # testcontainers, les démonstrations
make demo               # uniquement la mesure du verrou
```

## Ce que chaque thèse donne en code

| Thèse de l'article | Où c'est écrit | Ce qui le prouve |
|---|---|---|
| Une instruction unique est déjà atomique — pas de transaction | `service/company.go`, `service/user.go` | Les constructeurs `NewCompany` / `NewUser` ne prennent **pas** de `transaction.Manager` : l'erreur est impossible, pas seulement détectable (`service/crud_test.go`) |
| Un invariant sur plusieurs écritures — transaction indispensable | `service/onboarding.go` | `TestOnboarding_LeavesNothingBehindWhenItFails` : deux `INSERT` réussissent, le troisième échoue, la base est intacte |
| **Le piège : I/O réseau dans la transaction** | `service/verification.go` → `VerifyBad` | `TestVerifyBad_HoldsTheLockAcrossTheProviderCall` : un `UPDATE` concurrent attend **> 1 s** |
| **La correction : I/O dehors + `UPDATE` conditionnel** | `service/verification.go` → `VerifyGood` | `TestVerifyGood_HoldsNothingAcrossTheProviderCall` : le même `UPDATE` passe en **< 1 s** ; et `TestVerifyGood_IsIdempotentUnderConcurrency` : 6 appels simultanés → 1 succès, 5 conflits, une seule référence en base |
| `READ ONLY` explicite pour un instantané cohérent | `service/report.go` | `TestReadOnlyTransaction_RejectsWrite` (SQLSTATE `25006`) et `TestReport_AgreesWithItselfWhileTheDataChanges` |
| `SERIALIZABLE` impose une logique de retry | `postgres/transaction.go` → `ExecuteSerializable` | `TestExecuteSerializable_ReplaysARealSerializationFailure` : write skew forcé, `40001` réel, transaction rejouée |
| Une contrainte en base est un invariant qu'on ne défend plus à la main | `migrations/…_transactions_demo.up.sql` | `TestUser_UniqueConstraintsAreTranslated`, `TestMembership_ForeignKeysAreTranslated` |

## Les endpoints

```
GET    /healthz

POST   /api/companies                       ── écriture unique, aucune transaction
GET    /api/companies
GET    /api/companies/:id
PUT    /api/companies/:id
DELETE /api/companies/:id
POST|GET|PUT|DELETE /api/users[/:id]        ── idem

POST   /api/onboarding                      ── ✅ transaction : company + owner + membership
GET    /api/companies/:id/report            ── ✅ READ ONLY : trois lectures, un instantané
PUT    /api/companies/:id/members/:userId   ── ✅ SERIALIZABLE + retry : quota de sièges
DELETE /api/companies/:id/members/:userId

POST   /api/companies/:id/verify-bad        ── ❌ le piège
POST   /api/companies/:id/verify-good       ── ✅ la correction
```

## Reproduire l'incident à la main

Les trois terminaux, puis :

```bash
CID=$(curl -sS -X POST localhost:8080/api/companies \
  -H 'Content-Type: application/json' -d '{"name":"Cobaye SAS"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')

# ❌ pendant les 3 secondes qui suivent, la ligne est verrouillée
curl -sS -X POST localhost:8080/api/companies/$CID/verify-bad &

# dans un autre terminal, pendant ce temps
docker compose exec postgres psql -U postgres -d demo -c "
  SELECT pid, state, now() - xact_start AS age, wait_event, left(query,45)
  FROM pg_stat_activity
  WHERE xact_start IS NOT NULL AND datname = 'demo'
  ORDER BY xact_start;"
```

```
        state        | age_s |                     query
---------------------+-------+-----------------------------------------------
 idle in transaction |  1.33 | SELECT id, name, address, verification_status
```

`idle in transaction` sur un `SELECT … FOR UPDATE` : la connexion ne fait rien, mais elle tient son verrou, occupe une place dans le pool, et empêche `VACUUM` de recycler les *dead tuples* — sur toute la base, pas seulement sur cette table.

Refaites-le avec `verify-good` : la même requête ne montre rien. La transaction dure deux millisecondes.

Sous charge, la différence devient un incident :

```bash
hey -n 200 -c 20 -m POST http://localhost:8080/api/companies/$CID/verify-bad
```

## Les garde-fous à activer en production

Même avec de la discipline, un bug finit par ouvrir une transaction et l'oublier.

```sql
ALTER SYSTEM SET idle_in_transaction_session_timeout = '30s';
SET statement_timeout = '5s';   -- par session, ou SET LOCAL en transaction
SELECT pg_reload_conf();
```

## Le point de conception

Tout tient dans une seule abstraction, `postgres/pool.go` :

```go
type Executor interface {
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
```

`pgx.Tx` et `*pgxpool.Pool` la satisfont tous les deux. `TxManager.Executor(ctx)` renvoie la transaction portée par le contexte s'il y en a une, et le pool sinon. Un repository écrit contre `Executor` tourne donc à l'identique dans une transaction ou en autocommit.

Conséquence : **la décision d'ouvrir une transaction reste dans le service, à côté de l'invariant métier qui la justifie.** Sans ça, la solution de facilité est de faire passer chaque lecture et chaque écriture par un `BEGIN` dont elles n'ont pas besoin — ce que l'article appelle « entourer par précaution tout et n'importe quoi ».

Trois frontières, trois méthodes, et rien d'autre exposé au service :

| Méthode | Niveau d'isolation | Quand |
|---|---|---|
| `Execute` | défaut (`READ COMMITTED`) | Un invariant métier sur plusieurs écritures |
| `ExecuteReadOnly` | `REPEATABLE READ` + `READ ONLY` | Plusieurs lectures qui doivent s'accorder |
| `ExecuteSerializable` | `SERIALIZABLE` + retry sur `40001` | Une décision prise depuis une lecture qu'une écriture concurrente peut invalider |

`REPEATABLE READ` et non `READ COMMITTED` pour la lecture : sous `READ COMMITTED`, **chaque instruction prend un nouvel instantané**, donc deux `SELECT` d'une même transaction peuvent diverger — ce qui viderait de son sens la seule raison d'ouvrir une transaction en lecture. La première version du code faisait cette erreur ; `TestReadOnlyTransaction_SeesOneSnapshot` l'a attrapée.

## Et si on avait de la réplication ?

Question naturelle : avec des réplicas, ne faudrait-il pas imposer une transaction en lecture seule partout ?

Non — et le raisonnement vaut la peine d'être posé, parce que l'intuition inverse est répandue.

Le seuil ne bouge pas : **une transaction en lecture se justifie quand plusieurs lectures doivent s'accorder entre elles.** La réplication augmente la valeur du marquage `READ ONLY`, elle ne change pas quand il devient utile.

- **Le routage marche déjà pour une requête isolée.** Un `SELECT` autonome en autocommit est routable tel quel. Le cas ambigu, celui qui force le proxy à retomber sur le primaire, c'est un `SELECT` *à l'intérieur* d'une transaction dont il ignore le mode. C'est donc le bloc multi-requêtes qui a besoin du marquage.
- **La protection en écriture ne vient pas du `BEGIN`** mais de `default_transaction_read_only = on` posé sur la connexion vers le réplica. C'est un réglage de connexion : il couvre aussi l'autocommit.
- **Ça coûte.** Deux aller-retours par lecture, et en pooling par transaction la connexion reste attachée pendant tout le bloc au lieu d'être rendue après chaque instruction.

## La branche `feat/replication-routing`

`main` reste volontairement à un seul PostgreSQL. La démonstration exécutable de tout ce qui précède vit sur la branche **[`feat/replication-routing`](../../tree/feat/replication-routing)**, qui n'a pas vocation à être fusionnée : elle existe pour être lue et exécutée.

```bash
git switch feat/replication-routing
make db-up          # primaire 5432 + standby cloné par pg_basebackup, 5433
make db-seed        # 200 000 sociétés, 100 000 users, 140 000 rattachements
make test-integration
```

Elle ajoute un primaire + un standby dans `compose.yaml`, une option `postgres.WithReadReplica(pool)` qui route `ExecuteReadOnly` vers le standby, et un paquet `test/replication` qui **suspend le rejeu du WAL** (`pg_wal_replay_pause()`) pour rendre le retard déterministe plutôt que de courir après.

`git diff main..feat/replication-routing` ne touche ni `internal/service`, ni `internal/handler`, ni `internal/domain` — le routage lecture/écriture est une décision d'infrastructure, et le fait que la couche métier ne bouge pas est le résultat, pas un détail.

Deux choses que ces tests ont établies et qui corrigent l'intuition courante :

- **Le `25006` ne vient pas du `BEGIN`.** Posé sur une connexion au *primaire*, sans réplication et sans transaction explicite, `default_transaction_read_only = on` refuse déjà un `INSERT` en autocommit. Et un standby refuse l'écriture même si la session remet le réglage à `off` : un serveur en *recovery* ne sait pas écrire.
- **`DEFERRABLE` n'est pas un outil de standby** — l'inverse figurait ici même avant que les tests ne le démentent. `SERIALIZABLE READ ONLY DEFERRABLE` est rejeté sur un *hot standby* (`0A000`), et la forme sans `SERIALIZABLE` y est acceptée sans rien faire du tout (l'isolation reste `read committed`, or le mot-clé n'a d'effet que sous `SERIALIZABLE READ ONLY`). Ce qui protège une requête longue sur un réplica, ce sont `max_standby_streaming_delay` et `hot_standby_feedback`.

## Structure

```
cmd/server/          API Gin
cmd/remote/          faux prestataire externe, lent à dessein
internal/
  config/            koanf : application.yaml + surcharge par DEMO_*
  domain/            entités et erreurs sentinelles
  postgres/          Executor, TxManager, repositories
  service/           là où la frontière transactionnelle se décide
  handler/           Gin, DTO d'entrée, mapping erreurs → statuts
  gateway/           client HTTP vers cmd/remote
  migrate/           golang-migrate
  testutil/          conteneur PostgreSQL des tests
pkg/transaction/     UnitOfWork + Manager, sans dépendance au driver
test/integration/    les démonstrations
migrations/
```

## Configuration

`application.yaml` porte les valeurs par défaut. Toute clé se surcharge par variable d'environnement, préfixe `DEMO_`, double underscore pour la hiérarchie :

```bash
DEMO_POSTGRES__PASSWORD=… DEMO_POSTGRES__HOST=db.internal ./bin/server
```

## Ressources

- [PostgreSQL — Transaction Isolation](https://www.postgresql.org/docs/current/transaction-iso.html)
- [PostgreSQL — Explicit Locking](https://www.postgresql.org/docs/current/explicit-locking.html)
- [PostgreSQL — Hot Standby](https://www.postgresql.org/docs/current/hot-standby.html)
