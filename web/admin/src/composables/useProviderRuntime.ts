import { computed, ref } from "vue";
import {
  detectProviderProtocols,
  healthCheck,
  probeProviderModelProtocol,
  setProviderSuppress,
} from "../api.js";

export function useProviderRuntime(providerName: string) {
  const checking = ref(false);
  const healthResult = ref<any>(null);
  const detectingProtocols = ref(false);
  const selectedProbeModel = ref("");
  const selectedProbeProtocol = ref("chat");
  const protocolProbeResult = ref<any>(null);
  const exactProbing = ref(false);

  async function runHealthCheck() {
    checking.value = true;
    healthResult.value = null;
    try {
      healthResult.value = await healthCheck(providerName);
    } catch (e: any) {
      healthResult.value = { status: "error", error: e.message };
    } finally {
      checking.value = false;
    }
  }

  async function runProtocolDetect() {
    if (!providerName) return;
    detectingProtocols.value = true;
    try {
      await detectProviderProtocols(providerName);
    } finally {
      detectingProtocols.value = false;
    }
  }

  async function runExactProtocolProbe() {
    if (!providerName || !selectedProbeModel.value) return;
    exactProbing.value = true;
    protocolProbeResult.value = null;
    try {
      protocolProbeResult.value = await probeProviderModelProtocol(
        providerName,
        selectedProbeModel.value,
        selectedProbeProtocol.value
      );
    } finally {
      exactProbing.value = false;
    }
  }

  async function suppressProvider() {
    await setProviderSuppress(providerName, true);
  }

  async function unsuppressProvider() {
    await setProviderSuppress(providerName, false);
  }

  return {
    checking,
    healthResult,
    detectingProtocols,
    selectedProbeModel,
    selectedProbeProtocol,
    protocolProbeResult,
    exactProbing,
    runHealthCheck,
    runProtocolDetect,
    runExactProtocolProbe,
    suppressProvider,
    unsuppressProvider,
  };
}
