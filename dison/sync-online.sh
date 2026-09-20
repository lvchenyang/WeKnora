#!/usr/bin/env bash
# 线上：把工作区同步到 fork/dison/prod 最新。只动 git —— 构建和部署自己来。
#
#   bash dison/sync-online.sh
#
# 全文包在 { } 里，这条是必须的：下面的 git reset --hard 会把本文件自己
# 换成新版本，而 bash 是按字节偏移边读边执行的。不包的话文件一变，后半段
# 会被静默跳过，退出码还是 0 —— 看起来成功，实际只跑了一半。
set -euo pipefail
{
cd "$(dirname "$0")/.."

if [ "$(git rev-parse --abbrev-ref HEAD)" != "dison/prod" ]; then
  echo "✗ 当前在 $(git rev-parse --abbrev-ref HEAD)，本脚本只能在 dison/prod 上跑" >&2
  echo "  （否则 reset --hard 会把当前这条分支指到 dison/prod 去）" >&2
  exit 1
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "✗ 有被跟踪文件的本地改动，reset --hard 会把它们抹掉：" >&2
  git status -s >&2
  echo "  确认可以丢弃： git checkout -- . ，然后重跑" >&2
  exit 1
fi

git fetch fork
OLD=$(git rev-parse HEAD)
NEW=$(git rev-parse fork/dison/prod)
if [ "$OLD" = "$NEW" ]; then
  echo "✓ 已是最新（$(git rev-parse --short HEAD)），无事可做"
  exit 0
fi

CHANGED=$(git diff --name-only "$OLD" "$NEW")
git reset --hard "$NEW"
echo "✓ $(git rev-parse --short "$OLD") → $(git rev-parse --short "$NEW")，$(wc -l <<<"$CHANGED") 个文件"

# 下面只是打印提示，不执行任何构建。
# ponytail: 路径→服务的粗映射，漏判了就自己补 build。
changed() { grep -qE "$1" <<<"$CHANGED"; }
hint=()
if changed '^frontend/';                                                                 then hint+=(frontend);  fi
if changed '^(internal|cmd|migrations|config)/|^go\.(mod|sum)$|^docker/Dockerfile\.app$'; then hint+=(app);       fi
if changed '^docreader/|^docker/Dockerfile\.docreader$';                                 then hint+=(docreader); fi
if changed '^mcp-server/';                                                               then hint+=(mcp);       fi
if changed '^docker/Dockerfile\.sandbox$';                                               then hint+=(sandbox);   fi

if [ ${#hint[@]} -eq 0 ]; then
  echo "  改动不涉及任何服务的构建产物（文档/测试之类），不用重建"
else
  echo "  涉及的服务： ${hint[*]}"
  if changed '^frontend/'; then
    # 这里的 $(...) 故意在脚本里展开，打出来就能直接粘贴。
    # 不带这个变量不会坏事，只是系统信息页的 commit 显示 unknown
    # （Docker 构建上下文是 ./frontend，里面没有 .git，vite 自己取不到）。
    echo "  frontend 重建命令（带 commit 号，可直接粘贴）："
    echo "      VITE_FRONTEND_COMMIT=$(git rev-parse --short HEAD) docker compose build frontend"
  fi
fi
}
