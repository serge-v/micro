set -e
curl -O https://patch-diff.githubusercontent.com/raw/zyedidia/micro/pull/$1.patch
micro $1.patch
git am $1.patch
