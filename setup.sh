#!/bin/bash
set -e

echo "=== 環境確認とセットアップを開始します ==="

# Go確認
echo "Goバージョン確認:"
go version

# SQLite確認
echo "SQLite確認:"
sqlite3 --version

# GCC確認
echo "GCC確認:"
gcc --version

# go mod tidy の実行
echo "依存関係の解決 (go mod tidy)..."
go mod tidy

# logs確認・作成
echo "ログディレクトリの事前確認..."
if [ ! -d "logs" ]; then
    mkdir -p logs
    echo "-> logs ディレクトリを作成しました。"
else
    echo "-> logs ディレクトリは存在します。"
fi

echo "=== セットアップが正常に完了しました ==="