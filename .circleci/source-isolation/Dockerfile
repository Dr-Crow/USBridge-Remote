FROM cimg/base:2026.10@sha256:5c3b1d7f9597fe92f818ce7e075476a18ced9fbd1730b14382d4d01bfb76389e
USER root
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    ffmpeg xvfb python3 python3-cryptography libopus0 libgl1-mesa-dev xorg-dev libxkbcommon-dev \
    && rm -rf /var/lib/apt/lists/*
ENV HOME=/tmp LANG=C.UTF-8
USER circleci
