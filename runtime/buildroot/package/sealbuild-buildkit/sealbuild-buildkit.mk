################################################################################
#
# sealbuild-buildkit
#
################################################################################

SEALBUILD_BUILDKIT_VERSION = 0.31.1
SEALBUILD_BUILDKIT_SOURCE = buildkit-v$(SEALBUILD_BUILDKIT_VERSION).linux-amd64.tar.gz
SEALBUILD_BUILDKIT_SITE = https://github.com/moby/buildkit/releases/download/v$(SEALBUILD_BUILDKIT_VERSION)
SEALBUILD_BUILDKIT_LICENSE = Apache-2.0

define SEALBUILD_BUILDKIT_EXTRACT_CMDS
	$(TAR) -xzf $(SEALBUILD_BUILDKIT_DL_DIR)/$(SEALBUILD_BUILDKIT_SOURCE) \
		-C $(@D) --strip-components=1 bin/buildkitd
endef

define SEALBUILD_BUILDKIT_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/buildkitd $(TARGET_DIR)/usr/bin/buildkitd
endef

$(eval $(generic-package))
