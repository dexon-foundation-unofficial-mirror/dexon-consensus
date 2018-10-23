#!/bin/bash

if [ -e .dep/dkg ]; then
  exit 0
fi

if [ ! -d .dep/dkg ]; then
  mkdir -p .dep/dkg
  cd .dep/dkg
  git clone --depth 1 --single-branch -b dev git://github.com/dexon-foundation/bls.git &
  git clone --depth 1 git://github.com/dexon-foundation/mcl.git &
  wait
  cd bls
  make test_go -j
  cd ../../../
fi
cd vendor/github.com/dexon-foundation && rm -rf bls mcl
ln -s ../../../.dep/dkg/* .
