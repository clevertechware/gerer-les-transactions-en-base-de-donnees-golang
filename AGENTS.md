# AGENTS.md

Contexte pour les agents travaillant sur ce dépôt.

## Ce qu'est ce projet

Le compagnon de code d'un article sur les transactions PostgreSQL. Ce n'est pas
une application à faire grandir : **c'est une démonstration**. Chaque endpoint
existe pour illustrer une affirmation précise de l'article, et chaque affirmation
a un test qui la prouve contre un vrai PostgreSQL.

Avant d'ajouter quoi que ce soit, la question est : *quelle thèse de l'article
cela sert-il ?* Si la réponse est « aucune », c'est probablement hors périmètre.

## Stack

Go 1.26, Gin, pgx/v5, golang-migrate, koanf, testify + testcontainers, mockery.
PostgreSQL 18 (`uuidv7()` est un builtin de la 18, le schéma en dépend).

Deux binaires : `cmd/server` (l'API) et `cmd/remote` (un faux prestataire externe
qui répond après un délai configurable — sa lenteur est sa raison d'être).

## Architecture

Couches, dépendances vers l'intérieur :

```
handler → service → domain
              ↓
          postgres (repositories) → domain
```

Les interfaces sont déclarées **côté consommateur**, en minuscule :
`internal/service/ports.go` pour les repositories, chaque fichier de handler pour
son service. Le domaine n'exporte que des structs et des erreurs sentinelles.

`internal/handler` n'expose que du transport HTTP (gin) — pas de CLI, pas de
worker. Ça se voit dans le nom : chaque handler est `HTTP*Handler` dans un
fichier `http_*.go` (`HTTPCompanyHandler` dans `http_company.go`, etc.). Un
second transport, le jour où il existe, prend le même traitement plutôt que de
forcer un sous-paquet `internal/handler/http`.

`pkg/transaction` est le seul contrat public : `UnitOfWork` + `Manager`. Il ne
dépend d'aucun driver, et n'expose délibérément **aucun** moyen d'obtenir la
`pgx.Tx` sous-jacente. Un service décide *si* un travail est transactionnel,
jamais *comment*.

## Les deux règles à ne pas casser

**1. Un repository ne doit jamais exiger une transaction.**

Toutes les méthodes passent par `txManager.Executor(ctx)`, qui renvoie la
transaction du contexte s'il y en a une et le pool sinon. C'est ce qui permet aux
opérations à instruction unique de tourner en autocommit. Si un repository se met
à appeler `RequireTx`, chaque CRUD repart dans un `BEGIN` inutile et la démo
contredit l'article.

Seule exception, documentée : `CompanyRepository.LockForUpdate`, parce que
`SELECT … FOR UPDATE` relâche son verrou immédiatement en autocommit et ne
protège donc rien.

**2. Les services CRUD ne prennent pas de `transaction.Manager`.**

`NewCompany` et `NewUser` ont deux paramètres. Cette signature est la garantie —
plus forte qu'un test, puisqu'elle rend l'erreur impossible plutôt que
détectable. `internal/service/crud_test.go` la fige par une assertion de
compilation.

## Tests

```bash
make test-unit          # rapide, sans Docker (-short saute les conteneurs)
make test-integration   # testcontainers
make demo               # la mesure du verrou, isolée
```

- Unitaires : table-driven, `wantErr assert.ErrorAssertionFunc`, `t.Context()`,
  `logger.NewNoOpLogger()`, mocks mockery avec expecter.
- Pour mocker le `transaction.Manager`, utiliser les helpers de
  `internal/service/manager_test.go` : `passThroughManager` exécute l'unité de
  travail en direct, `newManagerExpecting(t, serializableOnly)` vérifie *quelle*
  frontière a été ouverte.
- Intégration : un conteneur par package via `testutil.RunWithPostgres` dans
  `TestMain`. Isolation par transaction rollbackée (`RepositorySuite.txContext`)
  pour les repositories, `testutil.Truncate` pour les tests concurrents.
- `internal/postgres` : les méthodes de `RepositorySuite` sont réparties un
  fichier de test par fichier source (`companies_test.go` pour
  `companies.go`, etc.), pas un seul fichier pour tout le paquet. Ce qui
  traverse plusieurs repositories (le test autocommit, `seedCompanyAndUser`)
  vit sur `suite_test.go`.

**Piège récurrent** : dans PostgreSQL, une instruction en échec avorte toute la
transaction. Un sous-test qui provoque une violation de contrainte ne peut donc
pas partager sa transaction avec le suivant — sinon tout ce qui suit revient en
`25P02` au lieu de l'erreur testée. Chaque sous-test ouvre la sienne.

**Autre piège** : dans un `t.Cleanup`, `t.Context()` est déjà annulé. Utiliser
`context.WithoutCancel(ctx)`, sinon le nettoyage ne part jamais.

## Migrations

`migrations/{timestamp}_{nom}.{up,down}.sql`, appliquées par `internal/migrate`.
Les fichiers `down` sont testés (`TestMigrations_DownThenUp`) : un down jamais
exécuté est un down qui ne marche pas, et on ne le découvre qu'au pire moment.

Le schéma porte volontairement beaucoup d'invariants (clés étrangères, uniques
partiels, `CHECK`) : c'est le « C » d'ACID en action, et chaque contrainte est un
invariant que le code applicatif n'a plus à défendre.

## Conventions

- Commentaires de code en anglais. README et ce fichier en français.
- Les commentaires ❌/✅ des chemins `verify-bad` / `verify-good` expliquent le
  *pourquoi* — ce sont eux que le lecteur de l'article vient lire. Ne pas les
  raccourcir.
- Erreurs : sentinelles dans `internal/domain`, préfixe `Err`. Le repository
  traduit le SQLSTATE, le service enveloppe une seule fois, le handler mappe vers
  un statut. Un 500 ne renvoie jamais la chaîne d'erreur interne.
- Handlers HTTP : struct `HTTP*Handler` + constructeur `NewHTTP*Handler`, un
  fichier `http_*.go` par ressource. Voir Architecture ci-dessus.
- `make mock` après toute modification d'interface. Le bloc `packages:` de
  `.mockery.yaml` est keyé sur le chemin du module (`go.mod`) : s'ils divergent,
  mockery ne matche plus rien et `make mock` ne régénère silencieusement aucun
  mock interne — `make mock-clean && make mock` puis `git status` doit montrer
  les mocks réécrits.

## Branche `feat/replication-routing`

Vous êtes sur cette branche. Elle porte la démonstration de la section
réplication de l'article ; `main` reste à un seul PostgreSQL et ne doit pas
recevoir ce code.

Le diff par rapport à `main` se limite à `compose.yaml`, `docker/primary/`,
`application.yaml`, `internal/config`, `internal/postgres/transaction.go`,
`cmd/server/main.go` et `test/replication/`. **S'il déborde sur
`internal/service`, `internal/handler` ou `internal/domain`, c'est que
l'abstraction a fui** — le routage lecture/écriture est une décision
d'infrastructure, la couche métier ne doit pas savoir qu'un réplica existe.

Invariants à ne pas casser :

- `WithReadReplica` est une option, pas un paramètre. Sans elle, `NewTxManager`
  se comporte exactement comme sur `main` (`replica` vaut alors `client`).
- Seul `ExecuteReadOnly` est routé. `Executor(ctx)` hors transaction reste sur le
  primaire : le manager ne peut pas savoir si l'instruction suivante écrit.
- Une lecture seule imbriquée dans une transaction en écriture rejoint la
  transaction ambiante, donc reste sur le primaire.
- `test/replication` monte sa propre topologie et n'utilise pas
  `internal/testutil.Shared` (un seul conteneur, pas de standby). Les tests
  suspendent le rejeu du WAL plutôt que de courir après le retard ; toute
  suspension doit être reprise dans un `t.Cleanup`, sinon les tests suivants
  lisent un standby figé.
