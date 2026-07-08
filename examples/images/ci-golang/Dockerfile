# 与 miniflow SSH 密钥挂载 (ssh_agent) 配合使用的 CI 基础镜像
FROM golang:1.25-alpine

RUN apk add --no-cache \
    git \
    openssh-client \
    make \
    ca-certificates

# 创建 uid 1000:1000 的用户（与 miniflow 容器的统一 UID 策略一致）
# OpenSSH 要求对应用户存在，否则拒绝启动
RUN addgroup -g 1000 ciuser && \
    adduser -u 1000 -G ciuser -h /tmp -s /bin/sh -D ciuser

# 预配置 known_hosts（系统级，所有用户可读）
RUN mkdir -p /etc/ssh && \
    ssh-keyscan github.com >> /etc/ssh/ssh_known_hosts && \
    ssh-keyscan gitlab.com >> /etc/ssh/ssh_known_hosts

WORKDIR /workspace
