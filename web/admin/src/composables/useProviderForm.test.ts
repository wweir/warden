import { describe, expect, test } from "bun:test";
import { nextTick, ref } from "vue";
import { useProviderForm } from "./useProviderForm";

describe("useProviderForm", () => {
  test("does not infer a preset when provider fields do not match one", () => {
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
    expect(form.currentPreset.value).toBe(null);
  });

  test("does not infer hard-coded provider types that are absent from form meta", () => {
    const providerFormMeta = ref({
      presets: [],
      service_protocol_templates: [],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "anthropic",
      url: "https://api.anthropic.com/v1",
      service_protocols: ["chat", "anthropic"],
    });

    expect(form.selectedPresetId.value).toBe("");
    expect(form.currentPreset.value).toBe(null);
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
    expect(form.isManagedCLIProxyAccess.value).toBe(true);

    form.providerConfig.value.backend_provider = "custom-cli";
    await nextTick();

    expect(form.selectedPresetId.value).toBe("");
    expect(form.isManagedCLIProxyAccess.value).toBe(false);
  });

  test("keeps provider fields without a preset hidden from the main form state", async () => {
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
    expect(form.currentPreset.value).toBe(null);
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

  test("does not fall back to a custom interface template", () => {
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

    expect(form.selectedServiceTemplateId.value).toBe("");

    form.handleServiceTemplateChange("chat_only");

    expect(form.providerConfig.value.service_protocols).toEqual(["chat"]);
    expect(form.providerConfig.value.responses_to_chat).toBe(false);
    expect(form.selectedServiceTemplateId.value).toBe("chat_only");
  });

  test("does not match a built-in interface template when responses_to_chat remains enabled", () => {
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
      service_protocols: ["chat"],
      responses_to_chat: true,
    });

    expect(form.selectedServiceTemplateId.value).toBe("");
  });

  test("matches interface templates regardless of service_protocols order", () => {
    const providerFormMeta = ref({
      presets: [],
      service_protocol_templates: [
        {
          id: "chat_responses_embeddings",
          families: ["openai"],
          service_protocols: ["chat", "responses", "embeddings"],
        },
      ],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "openai",
      url: "https://example.invalid/v1",
      service_protocols: ["responses", "embeddings", "chat"],
    });

    expect(form.selectedServiceTemplateId.value).toBe("chat_responses_embeddings");
  });

  test("clears selected interface template when it is no longer visible for the current provider", async () => {
    const providerFormMeta = ref({
      presets: [],
      service_protocol_templates: [
        {
          id: "chat_responses_embeddings",
          families: ["openai"],
          service_protocols: ["chat", "responses", "embeddings"],
        },
      ],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "openai",
      url: "https://example.invalid/v1",
      service_protocols: ["chat", "responses", "embeddings"],
    });
    expect(form.selectedServiceTemplateId.value).toBe("chat_responses_embeddings");

    form.providerConfig.value.family = "anthropic";
    await nextTick();

    expect(form.selectedServiceTemplateId.value).toBe("");
    expect(form.currentServiceTemplate.value).toBe(null);
  });

  test("applies the preset interface template when loading an old provider without service_protocols", () => {
    const providerFormMeta = ref({
      presets: [
        {
          id: "openai-compatible",
          family: "openai",
          service_protocol_template: "chat_responses_embeddings",
        },
      ],
      service_protocol_templates: [
        {
          id: "chat_responses_embeddings",
          families: ["openai"],
          service_protocols: ["chat", "responses", "embeddings"],
        },
      ],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "openai",
      url: "https://example.invalid/v1",
    });

    expect(form.selectedPresetId.value).toBe("openai-compatible");
    expect(form.selectedServiceTemplateId.value).toBe("chat_responses_embeddings");
    expect(form.providerConfig.value.service_protocols).toEqual(["chat", "responses", "embeddings"]);
  });

  test("matches anthropic-compatible providers regardless of endpoint URL", () => {
    const providerFormMeta = ref({
      presets: [
        {
          id: "anthropic-official",
          family: "anthropic",
          service_protocol_template: "anthropic_messages",
        },
      ],
      service_protocol_templates: [
        {
          id: "anthropic_messages",
          families: ["anthropic"],
          service_protocols: ["chat", "anthropic"],
        },
      ],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "anthropic",
      url: "https://anthropic-compatible.example/v1",
      service_protocols: ["anthropic", "chat"],
    });

    expect(form.selectedPresetId.value).toBe("anthropic-official");
    expect(form.selectedServiceTemplateId.value).toBe("anthropic_messages");
  });

  test("keeps explicit unmatched service_protocols when loading an existing provider", () => {
    const providerFormMeta = ref({
      presets: [
        {
          id: "openai-compatible",
          family: "openai",
          service_protocol_template: "chat_responses_embeddings",
        },
      ],
      service_protocol_templates: [
        {
          id: "chat_responses_embeddings",
          families: ["openai"],
          service_protocols: ["chat", "responses", "embeddings"],
        },
      ],
    });
    const form = useProviderForm(providerFormMeta);

    form.loadProviderConfig({
      family: "openai",
      url: "https://example.invalid/v1",
      service_protocols: ["responses"],
    });

    expect(form.selectedPresetId.value).toBe("openai-compatible");
    expect(form.selectedServiceTemplateId.value).toBe("");
    expect(form.providerConfig.value.service_protocols).toEqual(["responses"]);
  });
});
