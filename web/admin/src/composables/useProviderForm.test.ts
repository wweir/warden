import { describe, expect, test } from "bun:test";
import { ref } from "vue";
import { useProviderForm } from "./useProviderForm";

describe("useProviderForm", () => {
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
