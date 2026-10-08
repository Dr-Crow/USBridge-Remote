# The pinned stock streamer requires newer glibc/C++ ABI than the agent builder.
FROM ubuntu:24.04@sha256:f610ab94648195aa356059f5b41d6085c9d4d903c072430cdd1af7bdb646106b
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libstdc++6 libvulkan1 && rm -rf /var/lib/apt/lists/*
