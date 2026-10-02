idf_component_register(
    SRCS "main.c" "logbuf.c" "usb_core.c" "mux.c" "clone.c" "usbip.c" "ctrl.c"
    INCLUDE_DIRS "."
    REQUIRES usb
)

# TinyUSB is built as its own component and has to find our tusb_config.h.
idf_component_get_property(tusb_lib espressif__tinyusb COMPONENT_LIB)
target_include_directories(${tusb_lib} PUBLIC "${CMAKE_CURRENT_SOURCE_DIR}")
