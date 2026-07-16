################################################################################
#
# sealbuild-cni-plugins
#
################################################################################

SEALBUILD_CNI_PLUGINS_VERSION = 1.9.1
SEALBUILD_CNI_PLUGINS_SOURCE = cni-plugins-linux-amd64-v$(SEALBUILD_CNI_PLUGINS_VERSION).tgz
SEALBUILD_CNI_PLUGINS_SITE = https://github.com/containernetworking/plugins/releases/download/v$(SEALBUILD_CNI_PLUGINS_VERSION)
SEALBUILD_CNI_PLUGINS_LICENSE = Apache-2.0

define SEALBUILD_CNI_PLUGINS_EXTRACT_CMDS
	$(TAR) -xzf $(SEALBUILD_CNI_PLUGINS_DL_DIR)/$(SEALBUILD_CNI_PLUGINS_SOURCE) \
		-C $(@D) ./bridge ./loopback ./host-local ./firewall
endef

define SEALBUILD_CNI_PLUGINS_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/bridge $(TARGET_DIR)/usr/bin/buildkit-cni-bridge
	$(INSTALL) -D -m 0755 $(@D)/loopback $(TARGET_DIR)/usr/bin/buildkit-cni-loopback
	$(INSTALL) -D -m 0755 $(@D)/host-local $(TARGET_DIR)/usr/bin/buildkit-cni-host-local
	$(INSTALL) -D -m 0755 $(@D)/firewall $(TARGET_DIR)/usr/bin/buildkit-cni-firewall
endef

$(eval $(generic-package))
