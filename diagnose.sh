#!/bin/bash
echo "=== 診断スクリプト実行 ==="

echo "----------------------------------------"
echo "[OS情報]"
if [ -f /etc/os-release ]; then
    cat /etc/os-release | grep -E "^(NAME|VERSION)="
else
    uname -a
fi

echo "----------------------------------------"
echo "[Kernel情報]"
uname -r

echo "----------------------------------------"
echo "[Go バージョン]"
go version

echo "----------------------------------------"
echo "[Node.js バージョン]"
node -v 2>/dev/null || echo "Node.js が見つかりません"

echo "----------------------------------------"
echo "[npm バージョン]"
npm -v 2>/dev/null || echo "npm が見つかりません"

echo "----------------------------------------"
echo "[SQLite バージョン]"
sqlite3 --version 2>/dev/null || echo "SQLite3 が見つかりません"

echo "----------------------------------------"
echo "[Git ステータス]"
git status 2>/dev/null || echo "Gitリポジトリではないか、gitコマンドが使用できません"

echo "----------------------------------------"
echo "[DB存在確認]"
if [ -f "order.db" ]; then
    echo "order.db は存在します。サイズ: $(du -sh order.db | cut -f1)"
else
    echo "order.db はまだ生成されていません（アプリケーション起動時に自動作成されます）"
fi

echo "----------------------------------------"
echo "[logs存在確認]"
if [ -d "logs" ]; then
    echo "logs ディレクトリは存在します。"
    if [ -f "logs/order.log" ]; then
        echo "logs/order.log は存在します。サイズ: $(du -sh logs/order.log | cut -f1)"
    else
        echo "logs/order.log はまだ生成されていません。"
    fi
else
    echo "logs ディレクトリは存在しません。"
fi

echo "----------------------------------------"
echo "=== 診断完了 ==="