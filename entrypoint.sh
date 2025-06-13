#!/bin/sh

if [ ! -f "/app/data.db" ]; then
  echo "data.db not found, running migration..."
  go run migrate/migrate.go
fi

exec ./mop-backend-api
