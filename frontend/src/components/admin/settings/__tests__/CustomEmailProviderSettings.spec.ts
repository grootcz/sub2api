import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import CustomEmailProviderSettings from "../CustomEmailProviderSettings.vue";

vi.mock("vue-i18n", () => ({ useI18n: () => ({ t: (key: string) => key }) }));

const props = {
  baseUrl: "http://10.0.0.5:8081", token: "", tokenEdited: false,
  tokenConfigured: true, from: "billing@example.com", timeoutSeconds: 60,
};

describe("CustomEmailProviderSettings", () => {
  it("binds field values from props", () => {
    const w = mount(CustomEmailProviderSettings, { props });
    expect((w.get('[data-testid="custom-email-base-url"]').element as HTMLInputElement).value).toBe("http://10.0.0.5:8081");
    expect((w.get('[data-testid="custom-email-from"]').element as HTMLInputElement).value).toBe("billing@example.com");
    expect((w.get('[data-testid="custom-email-timeout"]').element as HTMLInputElement).value).toBe("60");
  });

  it("emits base url and from updates", async () => {
    const w = mount(CustomEmailProviderSettings, { props });
    await w.get('[data-testid="custom-email-base-url"]').setValue("http://10.0.0.6:8081");
    expect(w.emitted("update:baseUrl")?.[0]).toEqual(["http://10.0.0.6:8081"]);
    await w.get('[data-testid="custom-email-from"]').setValue("ops@example.com");
    expect(w.emitted("update:from")?.[0]).toEqual(["ops@example.com"]);
  });

  it("marks the token as edited on keydown and paste", async () => {
    const w = mount(CustomEmailProviderSettings, { props });
    const input = w.get('[data-testid="custom-email-token"]');
    await input.trigger("keydown");
    await input.trigger("paste");
    expect(w.emitted("update:tokenEdited")).toEqual([[true], [true]]);
    await input.setValue("ezm_new");
    expect(w.emitted("update:token")?.[0]).toEqual(["ezm_new"]);
  });

  it("renders the token input as a non-autofilled password field", () => {
    const input = mount(CustomEmailProviderSettings, { props }).get('[data-testid="custom-email-token"]');
    expect(input.attributes("type")).toBe("password");
    expect(input.attributes("autocomplete")).toBe("new-password");
  });

  it("emits the timeout as a number, or an empty string when cleared", async () => {
    const w = mount(CustomEmailProviderSettings, { props });
    const input = w.get('[data-testid="custom-email-timeout"]');
    await input.setValue("75");
    await input.setValue("");
    expect(w.emitted("update:timeoutSeconds")).toEqual([[75], [""]]);
  });

  it("switches the token hint by configured state", () => {
    expect(mount(CustomEmailProviderSettings, { props }).text()).toContain("admin.settings.smtp.custom.tokenConfiguredHint");
    expect(mount(CustomEmailProviderSettings, { props: { ...props, tokenConfigured: false } }).text()).toContain("admin.settings.smtp.custom.tokenHint");
  });
});
