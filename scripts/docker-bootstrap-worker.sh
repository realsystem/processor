#!/bin/sh
# Bootstrap script for Docker worker registration

set -e

echo "Running database migrations..."
/app/server -migrate

echo "Waiting for control-plane to be ready..."
until wget -q --spider http://control-plane:8080/api/v1/health; do
    sleep 1
done

echo "Control-plane is healthy!"
echo ""
echo "Worker will auto-register on first connection."
echo "Bootstrap complete."
