#!/usr/bin/env bash
# 本机：同步上游 → rebase dison/prod → 推 fork。只动 git，不碰任何服务。
#
#   git checkout dison/prod && bash dison/sync-local.sh
#
# 要加新定制：直接在 dison/prod 上改，commit，再跑本脚本。
#
# 全文包在 { } 里：bash 按字节偏移边读边执行，而本脚本自己就在 dison/prod
# 上、可能被 rebase 换掉。{ } 强制 bash 先把整块读完再执行；不包的话文件
# 一变，后半段会被静默跳过，而且退出码还是 0。
set -euo pipefail
{
cd "$(dirname "$0")/.."

# rebase 没收尾时 HEAD 是 detached，下面的分支检查会给出误导性建议，先拦掉
if [ -d "$(git rev-parse --git-path rebase-merge)" ] || [ -d "$(git rev-parse --git-path rebase-apply)" ]; then
  echo "✗ 上一次 rebase 还没做完。解完冲突后：" >&2
  echo "    git add <文件> && git rebase --continue" >&2
  echo "  放弃则： git rebase --abort" >&2
  exit 1
fi

if [ "$(git rev-parse --abbrev-ref HEAD)" != "dison/prod" ]; then
  echo "✗ 当前在 $(git rev-parse --abbrev-ref HEAD)，先 git checkout dison/prod" >&2
  exit 1
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "✗ 工作区有未提交改动，先 commit 掉再同步：" >&2
  git status -s >&2
  exit 1
fi

git fetch origin
git fetch fork

# 本地 main 快进到上游（不切分支）；fork/main 跟着走。两者都是纯快进。
git fetch origin main:main
git push fork origin/main:main

if ! git rebase origin/main; then
  echo >&2
  echo "✗ rebase 冲突（多半是 zh-CN.ts / Login.vue / index.html）。" >&2
  echo "  解完： git add <文件> && git rebase --continue，然后重跑本脚本" >&2
  echo "  放弃： git rebase --abort" >&2
  exit 1
fi

git push --force-with-lease fork dison/prod

echo
echo "✓ dison/prod = origin/main($(git rev-parse --short origin/main)) + $(git rev-list --count origin/main..dison/prod) 个定制提交"
git --no-pager log --oneline origin/main..dison/prod | sed 's/^/    /'
echo
echo "  线上接着跑： bash dison/sync-online.sh"
}
