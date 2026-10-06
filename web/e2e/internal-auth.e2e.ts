import { expect, test, type Page } from "@playwright/test";

// Sign-up, email verification, sign-in and password reset with the internal
// identity provider (AUTH_PROVIDER=internal). The emails are read from Mailpit.

test.skip(process.env.E2E_AUTH_PROVIDER !== "internal", "runs with E2E_AUTH_PROVIDER=internal");

const mailpit = process.env.MAILPIT_URL ?? "http://localhost:8025";
const email = `ana-${Date.now().toString(36)}@example.com`;

/** Waits for the newest email to `to` with `subject` and returns its link with ?token=. */
async function linkFromEmail(page: Page, subject: string): Promise<string> {
  let link = "";
  await expect(async () => {
    const search = await page.request.get(`${mailpit}/api/v1/search`, {
      params: { query: `to:"${email}" subject:"${subject}"` },
    });
    const { messages } = (await search.json()) as { messages: { ID: string }[] };
    expect(messages.length).toBeGreaterThan(0);
    const message = await page.request.get(`${mailpit}/api/v1/message/${messages[0].ID}`);
    const { Text } = (await message.json()) as { Text: string };
    link = Text.match(/https?:\/\/\S+\?token=[\w%-]+/)?.[0] ?? "";
    expect(link).not.toBe("");
  }).toPass({ timeout: 15_000 });
  return new URL(link).pathname + new URL(link).search;
}

test("an affiliate signs up, confirms the email and resets the password", async ({ page }) => {
  await page.goto("/entrar");
  await page.getByRole("link", { name: "Criar conta" }).click();
  await page.getByLabel("Nome").fill("Ana");
  await page.getByLabel("E-mail").fill(email);
  await page.getByLabel(/^Senha/).fill("senha-forte-1");
  await page.getByRole("button", { name: "Criar conta" }).click();
  await expect(page.getByRole("heading", { name: "Radar" })).toBeVisible();
  await expect(page.getByText("Confirme o seu e-mail")).toBeVisible();

  // The same email cannot sign up twice.
  const other = await (await page.context().browser()!.newContext()).newPage();
  await other.goto("/cadastro");
  await other.getByLabel("Nome").fill("Outra Ana");
  await other.getByLabel("E-mail").fill(email);
  await other.getByLabel(/^Senha/).fill("senha-forte-1");
  await other.getByRole("button", { name: "Criar conta" }).click();
  await expect(other.getByText("Já existe uma conta com este e-mail.")).toBeVisible();

  await page.goto(await linkFromEmail(page, "Confirme o seu e-mail"));
  await expect(page.getByText("E-mail confirmado.")).toBeVisible();
  await page.getByRole("link", { name: "Ir para o app" }).click();
  await expect(page.getByRole("heading", { name: "Radar" })).toBeVisible();
  await expect(page.getByText("Confirme o seu e-mail")).toHaveCount(0);

  await page.getByRole("button", { name: "Sair" }).click();
  await expect(page).toHaveURL(/\/entrar$/);
  await page.getByLabel("E-mail").fill(email);
  await page.getByLabel("Senha").fill("senha-errada");
  await page.getByRole("button", { name: "Entrar" }).click();
  await expect(page.getByText("E-mail ou senha incorretos.")).toBeVisible();

  await page.getByRole("link", { name: "Esqueci a senha" }).click();
  await page.getByLabel("E-mail").fill(email);
  await page.getByRole("button", { name: "Enviar link" }).click();
  await expect(page.getByText(`Se houver uma conta com ${email}`)).toBeVisible();
  await page.goto(await linkFromEmail(page, "Redefinir a sua senha"));
  await page.getByLabel(/^Nova senha/).fill("senha-nova-2");
  await page.getByRole("button", { name: "Trocar senha" }).click();
  await page.getByRole("link", { name: "Entre com a nova senha" }).click();
  await page.getByLabel("E-mail").fill(email);
  await page.getByLabel("Senha").fill("senha-nova-2");
  await page.getByRole("button", { name: "Entrar" }).click();
  await expect(page.getByRole("heading", { name: "Radar" })).toBeVisible();
});
