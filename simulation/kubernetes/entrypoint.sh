#!/bin/sh

if [ "$ROLE" = "validator" ]; then
  exec ./dexcon-simulation -config config.toml
elif [ "$ROLE" = "peer-server" ]; then
  exec ./dexcon-simulation-peer-server -config config.toml
fi
