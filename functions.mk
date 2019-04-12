# This function is put in a separate file to avoid breaking Vim syntax
# highlighting.
SAFE_STRING = '$(subst ','"'"',$1)'
