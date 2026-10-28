macro(configure_net_frontend _NAME_TARGET)

    target_compile_definitions(${_NAME_TARGET} PRIVATE -D USE_NET)
    message(STATUS "Support for net front end - Ready")

endmacro()
