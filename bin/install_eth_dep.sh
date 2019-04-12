#!/bin/sh

if [ -e .dep/libsecp256k1 ]; then
  exit 0
fi

rm -rf vendor/github.com/ethereum/go-ethereum/crypto/secp256k1/libsecp256k1
if [ ! -d .dep/libsecp256k1 ]; then
  git init .dep/libsecp256k1
  git -C .dep/libsecp256k1 remote add origin https://github.com/ethereum/go-ethereum.git
  git -C .dep/libsecp256k1 config core.sparsecheckout true
  echo 'crypto/secp256k1/libsecp256k1/*' >> .dep/libsecp256k1/.git/info/sparse-checkout
fi
git -C .dep/libsecp256k1 pull --depth=1 origin master
cp -r .dep/libsecp256k1/crypto/secp256k1/libsecp256k1 \
  vendor/github.com/ethereum/go-ethereum/crypto/secp256k1
