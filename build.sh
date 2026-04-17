#!/bin/bash

# ==============================================================================
# Windows 全局鼠标自然滚动开启工具 - 零配置交叉编译脚本
# 目标平台: Windows (amd64)
# ==============================================================================

set -e
set -u

# 配置参数
MODULE_NAME="natural_scroll"
APP_NAME="NaturalScroll_Pro"
OUTPUT_FILE="${APP_NAME}.exe"
SOURCE_FILE="main.go"

echo "============================================================"
echo " 🚀 开始构建: ${APP_NAME} (Target: Windows x86_64)"
echo "============================================================"

# 1. 检查 Go 环境
if ! command -v go &> /dev/null; then
    echo "错误: 未检测到 'go' 命令，请确认已安装 Golang。"
    exit 1
fi

# 2. 【新增】自动化模块管理 (Go Modules)
# 如果当前目录没有 go.mod 文件，说明是全新环境，自动初始化
if [ ! -f "go.mod" ]; then
    echo "📦 检测到全新环境，正在初始化 Go 模块..."
    go mod init "$MODULE_NAME"
fi

# 自动扫描源码并同步/下载所需的外部依赖 (如 x/sys/windows)
echo "正在检查并拉取依赖 (go mod tidy)..."
go mod tidy

# 3. 清理历史构建文件
if [ -f "$OUTPUT_FILE" ]; then
    echo "🧹 清理旧版本: ${OUTPUT_FILE}..."
    rm "$OUTPUT_FILE"
fi

# 4. 设置跨平台编译环境变量
export GOOS=windows
export GOARCH=amd64

# 5. 执行生产级编译
echo "正在交叉编译 (应用 -s -w 瘦身参数)..."
go build -ldflags "-s -w" -o "$OUTPUT_FILE" "$SOURCE_FILE"

# 6. 构建结果检查
if [ -f "$OUTPUT_FILE" ]; then
    FILE_SIZE=$(ls -lh "$OUTPUT_FILE" | awk '{print $5}')
    echo "编译成功！"
    echo "输出文件: $(pwd)/${OUTPUT_FILE} (体积: ${FILE_SIZE})"
    echo "============================================================"
else
    echo "编译失败，未生成可执行文件。"
    exit 1
fi
