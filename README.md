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

### Quick start (depuis zéro)

Enregistrez ce script et exécutez-le pour démarrer la démo complète :

```bash
#!/bin/bash
set -e

echo "🚀 Démarrage de la démo..."

# Démarrer PostgreSQL
echo "📦 Démarrage PostgreSQL..."
make db-up > /dev/null 2>&1
sleep 2

# Démarrer le prestataire externe (en arrière-plan)
echo "🔌 Démarrage du prestataire externe (port 9090)..."
make run-remote > /dev/null 2>&1 &
REMOTE_PID=$!
sleep 1

# Démarrer l'API (en arrière-plan)
echo "🌐 Démarrage de l'API (port 8080)..."
make run-server > /dev/null 2>&1 &
SERVER_PID=$!
sleep 2

# Vérifier que tout fonctionne
echo "✅ Vérification du serveur..."
if curl -sS http://localhost:8080/healthz > /dev/null; then
  echo "✅ Serveur prêt!"
  echo ""
  echo "📝 Créer une entreprise :"
  echo '  curl -X POST localhost:8080/api/companies -H "Content-Type: application/json" -d "{\"name\":\"Test SAS\"}"'
  echo ""
  echo "🧪 Exécuter les tests :"
  echo "  make test-unit          # tests rapides"
  echo "  make test-integration   # démonstrations complètes"
  echo "  make demo               # mesure du verrou"
  echo ""
  echo "Pour arrêter : make db-down"
else
  echo "❌ Le serveur n'a pas pu démarrer"
  kill $REMOTE_PID $SERVER_PID 2>/dev/null || true
  make db-down
  exit 1
fi

# Garder les processus en avant-plan
wait
```

Sauvegardez en `demo.sh`, rendez exécutable (`chmod +x demo.sh`), puis lancez avec `./demo.sh`.

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

**Cette branche en fait la démonstration exécutable.** `main` reste volontairement à un seul PostgreSQL.

### Ce que la branche ajoute

`compose.yaml` monte un standby (`pg_basebackup -R`, `default_transaction_read_only=on`) sur le port 5433. Côté code, une seule option :

```go
txManager := postgres.NewTxManager(log, primaryPool, postgres.WithReadReplica(replicaPool))
```

| Chemin | Destination | Pourquoi |
|---|---|---|
| `ExecuteReadOnly` | **réplica** | Le seul dont l'appelant a promis de ne pas écrire |
| `Execute`, `ExecuteSerializable` | primaire | Elles écrivent |
| `Executor(ctx)` hors transaction | primaire | Le manager ne sait pas si l'instruction suivante écrit |

La section `postgres.replica` d'`application.yaml` n'a besoin que de ce qui diffère du primaire ; le reste est hérité. Supprimez-la et le serveur retombe exactement sur le comportement de `main`.

`git diff main..feat/replication-routing` ne touche ni `internal/service`, ni `internal/handler`, ni `internal/domain`. C'est le point : le routage lecture/écriture est une décision d'infrastructure.

### Ce que ça coûte : `test/replication`

Les tests montent un vrai couple primaire/standby avec testcontainers, puis **suspendent le rejeu du WAL** (`pg_wal_replay_pause()`). Le retard devient contrôlé au lieu d'être couru après.

| Test | Ce qu'il établit |
|---|---|
| `TestExecuteReadOnly_ReadsAStandbyThatHasNotCaughtUp` | La ligne est *commitée* sur le primaire et `ExecuteReadOnly` ne la trouve pas |
| `TestExecute_SeesItsOwnWritesWhileTheStandbyLags` | Même manager, même standby en retard : le chemin en écriture lit ce qu'il a écrit |
| `TestAutocommitReadsStayOnThePrimary` | Un `GET` isolé n'est pas routé, sinon tout *read-your-writes* casse |

Conséquence pratique : un endpoint qui relit ce que la même requête vient d'écrire ne doit pas passer par `ExecuteReadOnly`. Le marquage explicite rend ce compromis visible dans le code — c'est sa vraie valeur, plus encore que le filet `25006`.

### Une correction à l'intuition courante

**Le `25006` ne vient pas du `BEGIN`.** `TestReadOnlyConnection_RefusesAWriteWithNoTransactionEverOpened` pose `default_transaction_read_only=on` sur une connexion **au primaire**, sans réplication et sans transaction explicite : l'`INSERT` en autocommit est refusé quand même. Et `TestStandby_RefusesAWriteEvenWithTheSettingTurnedOff` va plus loin — une session peut remettre le réglage à `off` sur le standby, l'écriture échoue toujours, parce qu'un serveur en recovery ne sait pas écrire. Le réglage est le refus poli ; la recovery est celui qui ne se discute pas.

Pour une requête analytique longue sur le standby, le risque n'est pas l'écriture mais l'annulation : le rejeu du WAL entre en conflit avec son instantané. Cela se règle avec `max_standby_streaming_delay` et `hot_standby_feedback`, côté serveur — rien à écrire dans l'application, donc rien à tester ici.

### À la main

```bash
make db-up                    # primaire 5432 + standby 5433
make db-seed                  # 200 000 sociétés, 100 000 users, 140 000 rattachements
make replication-status       # retard de rejeu, en octets
make test-integration         # inclut ./test/replication
```

`prefill_data_final.sql` sert à voir le routage sous une charge réaliste : un `GET /api/companies/:id/report` sur un jeu vide ne dit rien du tout. Le script `TRUNCATE` les trois tables avant de recharger, il n'est pas fait pour tourner sur autre chose qu'une base de démo.

Trois sociétés sur quatre restent en `pending`, sinon les endpoints `verify-bad` / `verify-good` n'ont plus rien à vérifier. La contrainte `companies_verification_ref_check` a d'ailleurs attrapé la première version du script, qui insérait `verified` sans référence — exactement ce que l'article dit d'attendre d'un invariant exprimé en contrainte.

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
