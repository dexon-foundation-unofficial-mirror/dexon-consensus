#!/bin/bash

if [ -e .dep/libsecp256k1 ]; then
  exit 0
fi

rm -rf vendor/github.com/dexon-foundation/dexon/crypto/secp256k1/libsecp256k1
if [ ! -d .dep/libsecp256k1 ]; then
  git init .dep/libsecp256k1
  cd .dep/libsecp256k1
  git remote add origin https://github.com/dexon-foundation/dexon.git
  git config core.sparsecheckout true
  echo "crypto/secp256k1/libsecp256k1/*" >> .git/info/sparse-checkout
  cd ../../
fi
cd .dep/libsecp256k1
git pull --depth=1 origin master
cd ../../
cp -r .dep/libsecp256k1/crypto/secp256k1/libsecp256k1 \
  vendor/github.com/dexon-foundation/dexon/crypto/secp256k1
