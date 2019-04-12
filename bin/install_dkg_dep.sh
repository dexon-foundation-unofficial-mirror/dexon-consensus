#!/bin/sh

: "${MAKE:="make"}"

if [ -e .dep/dkg ]; then
  exit 0
fi

if [ ! -d .dep/dkg ]; then
  mkdir -p .dep/dkg
  (
    cd .dep/dkg || exit
    git clone --depth 1 -b master git://github.com/dexon-foundation/bls.git &
    git clone --depth 1 -b master git://github.com/dexon-foundation/mcl.git &
    wait
    cd bls || exit
    eval "${MAKE} test_go -j MCL_USE_OPENSSL=0"
  ) || exit
fi
cd vendor/github.com/dexon-foundation && rm -rf bls mcl
ln -s ../../../.dep/dkg/* .
