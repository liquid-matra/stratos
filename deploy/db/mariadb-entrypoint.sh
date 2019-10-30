#!/bin/bash
set -e

MYSQL_DATADIR="/var/lib/mysql"

if [ ! -d "$MYSQL_DATADIR/mysql" ]; then
  # if [ -z "$MYSQL_ROOT_PASSWORD" -a -z "$MYSQL_ALLOW_EMPTY_PASSWORD" ]; then
  #   echo >&2 'error: database is uninitialized and MYSQL_ROOT_PASSWORD not set'
  #   echo >&2 '  Did you forget to add -e MYSQL_ROOT_PASSWORD=... ?'
  #   exit 1
  # fi

  echo 'Running mysql_install_db ...'
  mysql_install_db --user=mysql --datadir="$MYSQL_DATADIR"
  echo 'Finished mysql_install_db'

  # These statements _must_ be on individual lines, and _must_ end with
  # semicolons (no line breaks or comments are permitted).
  # TODO proper SQL escaping on ALL the things D:

 tempSqlFile='/tmp/mysql-first-time.sql'
  cat > "$tempSqlFile" <<-EOSQL
DELETE FROM mysql.user ;
CREATE USER 'root'@'%' IDENTIFIED BY '${MYSQL_ROOT_PASSWORD}' ;
GRANT ALL ON *.* TO 'root'@'%' WITH GRANT OPTION ;
DROP DATABASE IF EXISTS test ;
EOSQL

  if [ "$MYSQL_DATABASE" ]; then
    echo "CREATE DATABASE IF NOT EXISTS \`$MYSQL_DATABASE\` ;" >> "$tempSqlFile"
  fi

  if [ "$MYSQL_USER" -a "$MYSQL_PASSWORD" ]; then
    echo "CREATE USER '$MYSQL_USER'@'%' IDENTIFIED BY '$MYSQL_PASSWORD' ;" >> "$tempSqlFile"

    if [ "$MYSQL_DATABASE" ]; then
      echo "GRANT ALL ON \`$MYSQL_DATABASE\`.* TO '$MYSQL_USER'@'%' ;" >> "$tempSqlFile"
    fi
  fi

  if [ "$DB_DATABASE_NAME" ]; then
    echo "CREATE DATABASE IF NOT EXISTS \`$DB_DATABASE_NAME\` ;" >> "$tempSqlFile"
    echo "CREATE USER $DB_USER IDENTIFIED BY '$DB_PASSWORD' ;" >> "$tempSqlFile"
    echo "GRANT ALL ON \`$DB_DATABASE_NAME\`.* TO '$DB_USER'@'%' ;" >> "$tempSqlFile"
  fi

  echo 'FLUSH PRIVILEGES ;' >> "$tempSqlFile"
  set -- "$@" --init-file="$tempSqlFile"
fi

chown -R mysql:mysql "$MYSQL_DATADIR"
mkdir /var/run/mysql
chown -R mysql:mysql /var/run/mysql

exec "$@"
