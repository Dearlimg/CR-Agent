# CR-Agent 运行镜像
#
# 目标服务器只有 1.6G 内存且无 swap，不适合在容器内编译 Go，
# 因此沿用本项目既有的部署方式：本地交叉编译 → 只 COPY 二进制。
#
# 构建前先在本地（Windows）产出 Linux 二进制：
#   $env:CGO_ENABLED="0"; $env:GOOS="linux"; $env:GOARCH="amd64"
#   go build -trimpath -ldflags="-s -w" -o cr-agent ./cmd/server
#   docker build -t cr-agent:latest .
#
# 运行时配置（PORT / MYSQL_DSN / DEEPSEEK_API_KEY 等）不写进镜像，
# 由 docker run --env-file /opt/cr-agent/.env 注入。

FROM alpine:3.20

# 换阿里云源；DeepSeek 走 HTTPS，ca-certificates 必需
RUN sed -i 's#https\?://dl-cdn.alpinelinux.org#https://mirrors.aliyun.com#g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

WORKDIR /app

COPY cr-agent /app/cr-agent
COPY web /app/web
COPY skills /app/skills

RUN chmod +x /app/cr-agent && mkdir -p /app/data

ENV TZ=Asia/Shanghai

EXPOSE 80
CMD ["/app/cr-agent"]
