################################################################################
#
# sealbuild-runc
#
################################################################################

SEALBUILD_RUNC_VERSION = 1.5.1
SEALBUILD_RUNC_SOURCE = runc.amd64
SEALBUILD_RUNC_SITE = https://github.com/opencontainers/runc/releases/download/v$(SEALBUILD_RUNC_VERSION)
SEALBUILD_RUNC_LICENSE = Apache-2.0

define SEALBUILD_RUNC_EXTRACT_CMDS
	cp $(SEALBUILD_RUNC_DL_DIR)/$(SEALBUILD_RUNC_SOURCE) $(@D)/runc
endef

define SEALBUILD_RUNC_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/runc $(TARGET_DIR)/usr/bin/runc
endef

$(eval $(generic-package))
