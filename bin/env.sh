# Environment variables for the project.
# shellcheck shell=dash disable=SC2155

export GITROOT=$(git rev-parse --show-toplevel)

# Setup custom build flags for non-root homebrew installation.
if [ "$(uname -o)" = "Darwin" ] && [ "$(brew --prefix)" != "/usr/local" ]; then
  export BLS256_SLIB_LDFLAGS="-L$(brew --prefix gmp)/lib"
  export BLS384_SLIB_LDFLAGS="-L$(brew --prefix gmp)/lib"

  export CFLAGS="-I$(brew --prefix gmp)/include -I$(brew --prefix openssl)/include $CFLAGS"
  export LDFLAGS="-L$(brew --prefix gmp)/lib $LDFLAGS -L$(brew --prefix openssl)/lib $LDFLAGS"
  export CGO_LDFLAGS=$LDFLAGS
  export LD_LIBRARY_PATH=$GITROOT/lib
  export DYLD_LIBRARY_PATH=$LD_LIBRARY_PATH
fi
