#!/bin/sh
set -eu

# Upgrade existing root-owned volumes without replacing the private encryption key.
mkdir -p /app/data
chown -R app:app /app/data
chmod 0700 /app/data

# Supervisor keeps the nginx master as root and drops the backend to user app.
exec "$@"
