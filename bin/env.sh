# Environment variables for the project.

# Setup custom build flags for non-root homebrew installation.
if [ "$(uname -o)" = "Darwin" ] && [ "$(brew --prefix)" != "/usr/local" ]; then
  export BLS256_SLIB_LDFLAGS="-L$(brew --prefix gmp)/lib"
  export BLS384_SLIB_LDFLAGS="-L$(brew --prefix gmp)/lib"

  export CFLAGS="-I$(brew --prefix gmp)/include -I$(brew --prefix openssl)/include $CFLAGS"
  export LDFLAGS="-L$(brew --prefix gmp)/lib $LDFLAGS -L$(brew --prefix openssl)/lib $LDFLAGS"
  export CGO_LDFLAGS=$LDFLAGS
fi
