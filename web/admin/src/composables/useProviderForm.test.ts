import { describe, expect, test } from "bun:test";
import { nextTick, ref } from "vue";
import { useProviderForm } from "./useProviderForm";

describe("useProviderForm", () => {
  test("does not use a custom access type when provider fields do not match a preset", () => {
    const providerFormMeta = ref({
      presets: [
        {
          id: "openai-compatible",
          family: "openai",
        },
      ],
      service_protocol_templates: [],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "openai",
      backend: "cliproxy",
      backend_provider: "custom-cli",
      url: "http://127.0.0.1:18741/v1",
      service_protocols: ["chat"],
    });

    expect(form.selectedPresetId.value).toBe("");
    expect(form.showAdapterAdvanced.value).toBe(true);
    expect("CUSTOM_ACCESS_TYPE" in form).toBe(false);
  });

  test("keeps managed cliproxy access tied to a matched preset", async () => {
    const providerFormMeta = ref({
      presets: [
        {
          id: "cliproxy-codex",
          family: "openai",
          backend: "cliproxy",
          backend_provider: "codex",
        },
      ],
      service_protocol_templates: [],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "openai",
      backend: "cliproxy",
      backend_provider: "codex",
      url: "http://127.0.0.1:18741/v1",
      service_protocols: ["chat"],
    });

    expect(form.selectedPresetId.value).toBe("cliproxy-codex");
    expect(form.showAdapterAdvanced.value).toBe(false);
    expect(form.isManagedCLIProxyAccess.value).toBe(true);

    form.providerConfig.value.backend_provider = "custom-cli";
    await nextTick();

    expect(form.selectedPresetId.value).toBe("");
    expect(form.showAdapterAdvanced.value).toBe(true);
    expect(form.isManagedCLIProxyAccess.value).toBe(false);
  });

  test("uses the effective auth source after switching manual fields to cliproxy", async () => {
    const providerFormMeta = ref({
      presets: [],
      service_protocol_templates: [],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "openai",
      url: "https://example.invalid/v1",
      api_key: "secret",
      service_protocols: ["chat"],
    });
    form.selectedAuthSource.value = "api_key";

    form.providerConfig.value.backend = "cliproxy";
    form.providerConfig.value.backend_provider = "codex";
    await nextTick();

    expect(form.effectiveAuthSource.value).toBe("none");

    const provider = { ...form.providerConfig.value };
    form.applyProviderAuthSource(provider, form.effectiveAuthSource.value);

    expect(provider.api_key).toBeUndefined();
    expect(provider.__clear_api_key__).toBe(true);
  });

  test("clears backend fields when provider family no longer supports them", async () => {
    const providerFormMeta = ref({
      presets: [],
      service_protocol_templates: [],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "openai",
      backend: "cliproxy",
      backend_provider: "codex",
      url: "http://127.0.0.1:18741/v1",
      service_protocols: ["chat"],
    });

    form.providerConfig.value.family = "anthropic";
    await nextTick();

    expect(form.providerConfig.value.backend).toBe("");
    expect(form.providerConfig.value.backend_provider).toBe("");
    expect(form.effectiveAuthSource.value).toBe("api_key");
  });

  test("clears responses_to_chat when switching from custom interfaces to a template", () => {
    const providerFormMeta = ref({
      presets: [],
      service_protocol_templates: [
        {
          id: "chat_only",
          families: ["openai"],
          service_protocols: ["chat"],
        },
      ],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "openai",
      url: "https://example.invalid/v1",
      service_protocols: ["responses"],
      responses_to_chat: true,
    });

    expect(form.selectedServiceTemplateId.value).toBe(form.CUSTOM_SERVICE_TEMPLATE);

    form.handleServiceTemplateChange("chat_only");

    expect(form.providerConfig.value.service_protocols).toEqual(["chat"]);
    expect(form.providerConfig.value.responses_to_chat).toBe(false);
    expect(form.selectedServiceTemplateId.value).toBe("chat_only");
  });
});
