#!/bin/sh

if [ "$ROLE" = "node" ]; then
  exec ./dexcon-simulation -config config.toml
elif [ "$ROLE" = "peer-server" ]; then
  exec ./dexcon-simulation-peer-server -config config.toml
elif [ "$ROLE" = "scheduler" ]; then
  exec ./dexcon-simulation-with-scheduler -config config.toml
fi
