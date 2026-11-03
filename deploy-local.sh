#!/usr/bin/env bash

set -Eeuo pipefail

fail() {
	printf '错误：%s\n' "$1" >&2
	exit 1
}

printf '准备本地部署 Sub2API...\n'

if [[ ! -d sub2apiData ]]; then
	mkdir -p sub2apiData || fail '创建 sub2apiData 文件夹失败。'
	printf '已创建 sub2apiData 文件夹。\n'
fi

printf '切换到 release 分支...\n'
git switch release || fail '切换到 release 分支失败。'

printf '更新 release 分支至远端最新版本...\n'
git pull --ff-only origin release || fail '更新 release 分支失败。'

printf '构建本地 Docker 镜像...\n'
docker build -t sub2api:local . || fail 'Docker 镜像构建失败。'

printf '生成 .env 环境变量文件...\n'
cp .env.example .env || fail '复制 .env.example 到 .env 失败。'

cat <<'EOF'

已生成 .env 环境变量文件，请确认已修改以下变量：
BIND_HOST、
POSTGRES_HOST、POSTGRES_USER、POSTGRES_PASSWORD、DATABASE_PORT、
REDIS_HOST、REDIS_PORT、REDIS_PASSWORD、
ADMIN_EMAIL、ADMIN_PASSWORD
EOF

read -r -p '确认已完成修改并继续启动？请输入 y：' confirmation || fail '未能读取确认输入，已停止部署。'
if [[ "$confirmation" != 'y' ]]; then
	printf '未输入 y，已停止部署；请修改 .env 后重新运行脚本。\n'
	exit 1
fi

printf '启动 Docker Compose 服务...\n'
docker compose up -d || fail 'Docker Compose 启动失败。'

printf '本地部署完成。\n'
