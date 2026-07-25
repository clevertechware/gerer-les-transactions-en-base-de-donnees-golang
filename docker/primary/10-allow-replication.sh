#!/bin/sh
# initdb's default pg_hba.conf only accepts replication connections from
# localhost, so the standby container cannot clone the primary without this.
#
# "trust" is fine for a demo on a private compose network. In production this
# would be a dedicated role with a password and a restricted CIDR.
set -e

echo "host replication all all trust" >> "$PGDATA/pg_hba.conf"
