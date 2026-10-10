# Ephemeral runtime image only. Never push/save it as an artifact.
# Ubuntu 22.04 matches the hosted machine ABI used to build the existing viewer.
FROM ubuntu:22.04@sha256:08ea48a03a3e78ebc7cd526e6a275053223aadd88bfc09cc49b06d5281525fde
USER root
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    python3 python3-dbus dbus-daemon xvfb xauth xdotool x11-utils x11-xserver-utils \
    libgl1-mesa-dri libgl1 libx11-6 libxcursor1 libxrandr2 libxinerama1 libxi6 libxxf86vm1 \
    libopus0 libssl3 libavcodec58 libavutil56 libswscale5 libpulse0 libva2 libvulkan1 \
    libxkbcommon0 fonts-dejavu-core ffmpeg ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 fixture && useradd --uid 10001 --gid 10001 --no-create-home fixture
COPY agent/ /opt/agent/
COPY components/ /opt/preview/components/
COPY gate/ /opt/gate/
RUN chmod -R a+rX /opt/agent /opt/preview /opt/gate
USER 10001:10001
ENV PATH=/usr/bin:/bin LANG=C.UTF-8 HOME=/work/home DISPLAY=:97 XAUTHORITY=/work/xauthority \
    FYNE_SCALE=1 LIBGL_ALWAYS_SOFTWARE=1 SOURCE_PREVIEW_ENGINE_ACCEPTANCE=1 SOURCE_PREVIEW_ENGINE_WORK=/work
WORKDIR /work
ENTRYPOINT ["/usr/bin/python3", "/opt/gate/engine_preview_container.py"]
