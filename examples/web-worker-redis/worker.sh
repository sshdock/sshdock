#!/bin/sh
set -eu

while true; do
  redis-cli -h redis ping
  sleep 15
done
