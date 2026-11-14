#!/bin/sh
set -eu

# Initialize the mounted data directory for the backend user.
mkdir -p /app/data
chown app:app /app/data
chmod 0700 /app/data

# Supervisor keeps the nginx master as root and drops the backend to user app.
exec "$@"
