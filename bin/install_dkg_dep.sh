#!/bin/bash

if [ -e .dep/dkg ]; then
  exit 0
fi

rm -rf vendor/github.com/herumi/*
if [ ! -d .dep/dkg ]; then
  mkdir -p .dep/dkg
  cd .dep/dkg
  git clone --depth 1 git://github.com/herumi/xbyak.git &
  git clone --depth 1 git://github.com/herumi/cybozulib.git &
  git clone --depth 1 --single-branch -b dev git://github.com/Spiderpowa/bls.git &
  git clone --depth 1 git://github.com/herumi/mcl.git &
  wait
  if [ "$(uname -o)" = "Darwin" ] && [ "$(brew --prefix)" != "/usr/local" ]; then
    cd mcl
    git am ../../../bin/patches/0001-common.mk-remove-hard-coded-CFLAGS-and-LDFLAGS.patch
    cd ..
  fi
  cd bls
  make test_go -j
  cd ../../../
fi
cp -r .dep/dkg/* \
  vendor/github.com/Spiderpowa
mkdir -p lib
cd lib
ln -sf ../vendor/github.com/Spiderpowa/bls/lib/* .
