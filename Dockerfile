FROM golang:1.20-alpine AS builder

# 安装构建依赖
RUN apk add --no-cache git

# 设置工作目录
WORKDIR /app

# 复制Go模块文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源码
COPY . .

# 编译
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o image-size-gatekeeper ./cmd/main.go

# 使用轻量级基础镜像
FROM alpine:3.17

# 安装skopeo和依赖
RUN apk add --no-cache skopeo ca-certificates

# 复制构建好的二进制文件
COPY --from=builder /app/image-size-gatekeeper /usr/local/bin/

# 设置执行权限
RUN chmod +x /usr/local/bin/image-size-gatekeeper

# 运行应用
ENTRYPOINT ["/usr/local/bin/image-size-gatekeeper"]
