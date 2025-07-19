FROM golang:1.21 as builder

WORKDIR /workspace

# 复制go模块文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY cmd/ cmd/
COPY pkg/ pkg/

# 编译
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o imagesizegatekeeper cmd/main.go

# 使用轻量级的基础镜像
FROM alpine:3.17

# 安装CA证书
RUN apk --no-cache add ca-certificates

WORKDIR /

# 从构建阶段复制二进制文件
COPY --from=builder /workspace/imagesizegatekeeper .

# 设置用户
USER 65532:65532

ENTRYPOINT ["/imagesizegatekeeper"] 