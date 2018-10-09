#!/bin/bash

if [ -e .dep/dkg ]; then
  exit 0
fi

if [ ! -d .dep/dkg ]; then
  mkdir -p .dep/dkg
  cd .dep/dkg
  git clone --depth 1 git://github.com/herumi/xbyak.git &
  git clone --depth 1 git://github.com/herumi/cybozulib.git &
  git clone --depth 1 --single-branch -b dev git://github.com/Spiderpowa/bls.git &
  git clone --depth 1 git://github.com/herumi/mcl.git &
  wait
  cd bls
  make test_go -j
  cd ../../../
fi
cd vendor/github.com/Spiderpowa && rm -rf *
ln -s ../../../.dep/dkg/* .
