import { expect, test, type Browser, type Page } from "@playwright/test";

// End-to-end walkthrough of the MVP through the screens: the mentor creates the
// mentorship, invites an affiliate and publishes a list; she imports it, gets
// her own link, sees her results and authorizes the mentor; the mentor sees the
// cohort and pays the subscription. Unique names per run, so it runs again on
// the same database.

const suffix = Date.now().toString(36);
const mentor = `mentor-${suffix}`;
const affiliate = `ana-${suffix}`;
const mentorship = `Turma ${suffix}`;
const list = `Achados ${suffix}`;

test.skip((process.env.E2E_AUTH_PROVIDER ?? "dev") !== "dev", "the MVP walkthrough runs with AUTH_PROVIDER=dev");

async function signInAs(browser: Browser, name: string): Promise<Page> {
  const page = await (await browser.newContext()).newPage();
  await page.goto("/entrar");
  await page.getByLabel("Ambiente local: entrar como").fill(name);
  await page.getByRole("button", { name: "Entrar" }).click();
  await expect(page.getByRole("heading", { name: "Radar" })).toBeVisible();
  return page;
}

/** Accepts the next window.confirm of the page. */
function acceptConfirm(page: Page) {
  page.once("dialog", (d) => void d.accept());
}

test("mentor and affiliate go through the app from the invite to the subscription", async ({ browser }) => {
  // The mentor signs in, sees the radar of the personal workspace and creates the mentorship.
  const m = await signInAs(browser, mentor);
  // The worker collects the (mock) catalog on start; wait for the first collection.
  await expect(async () => {
    await m.reload();
    await expect(m.getByText(/Atualizado (há|agora)/)).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 60_000 });
  await expect(m.getByRole("button", { name: "Salvar" }).first()).toBeVisible();
  await m.getByTitle("Mentoria").click();
  await m.getByLabel("Nome da mentoria").fill(mentorship);
  await m.getByRole("button", { name: "Criar mentoria" }).click();
  await expect(m.getByRole("heading", { name: mentorship })).toBeVisible();

  // Generate the invite and take the link.
  await m.getByRole("button", { name: "Gerar convite" }).click();
  const link = await m.locator("input[readonly]").inputValue();
  expect(link).toMatch(/\/convite\/[\w-]+$/);
  await expect(m.getByText("Convites pendentes")).toBeVisible();

  // The affiliate opens the link signed out, signs in and accepts.
  const a = await (await browser.newContext()).newPage();
  await a.goto(new URL(link).pathname);
  await expect(a.getByRole("heading", { name: mentorship })).toBeVisible();
  await a.getByRole("button", { name: "Entrar para aceitar" }).click();
  await a.getByLabel("Ambiente local: entrar como").fill(affiliate);
  await a.getByRole("button", { name: "Entrar" }).click();
  await a.getByRole("button", { name: "Aceitar convite" }).click();
  await expect(a.getByRole("heading", { name: "Listas" })).toBeVisible();
  await expect(a.getByText("O seu mentor ainda não publicou listas.")).toBeVisible();

  // A used invite does not work again.
  const other = await (await browser.newContext()).newPage();
  await other.goto(new URL(link).pathname);
  await expect(other.getByText("Este convite já foi usado.")).toBeVisible();

  // The mentor sees the affiliate in the cohort.
  await m.reload();
  await expect(m.getByText(`${affiliate}@dev.local`)).toBeVisible();

  // The affiliate connects the (mock) Shopee account.
  await a.getByRole("link", { name: "Shopee" }).click();
  await a.getByLabel("AppID").fill("18300001234");
  await a.getByLabel("Secret").fill("segredo-de-teste");
  await a.getByRole("button", { name: "Conectar", exact: true }).click();
  await expect(a.getByText("Conectada", { exact: true })).toBeVisible();
  await expect(a.getByText(/AppID ••••1234/)).toBeVisible();

  // The mentor builds the list with a radar product and publishes it.
  await m.getByTitle("Listas").click();
  await m.getByRole("button", { name: "Nova lista" }).click();
  await m.getByLabel("Título").fill(list);
  await m.getByRole("button", { name: "Criar e adicionar produtos" }).click();
  await expect(m.getByRole("heading", { name: list })).toBeVisible();
  await expect(m.getByText("Rascunho", { exact: true })).toBeVisible();
  await m.getByLabel("Adicionar produto").fill("fone");
  await m.getByRole("button", { name: "Adicionar" }).first().click();
  await m.getByLabel("Dica para a turma").fill("Vende muito no reels");
  await m.getByLabel("Dica para a turma").blur();
  await expect(m.getByText("Salvando…")).toHaveCount(0);
  acceptConfirm(m);
  await m.getByRole("button", { name: "Publicar para a turma" }).click();
  await expect(m.getByText("Lista publicada. A turma está sendo avisada.")).toBeVisible();

  // The affiliate gets the notice and imports the list with her own link.
  await a.goto("/");
  await expect(a.getByRole("link", { name: /Notificações \(1 não lidas\)/ })).toBeVisible();
  await a.getByRole("link", { name: /Notificações/ }).click();
  await a.getByText(`Nova lista: ${list}`).click();
  await expect(a.getByRole("heading", { name: list })).toBeVisible();
  await a.getByRole("button", { name: "Importar a lista para a minha coleção" }).click();
  await expect(a.getByRole("button", { name: "Tudo já está na sua coleção" })).toBeVisible();
  await expect(async () => {
    await a.reload();
    await expect(a.getByText("Link pronto")).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 30_000 });

  // In the collection, the item has the mentor's tip and one link per channel.
  await a.getByRole("link", { name: "Na sua coleção" }).click();
  await expect(a.getByRole("heading", { name: "Link de afiliado" })).toBeVisible();
  await expect(a.getByLabel("Notas (só você vê)")).toHaveValue("Dica do mentor: Vende muito no reels");
  await expect(a.getByRole("button", { name: /Copiar link do Instagram/ })).toBeVisible();
  await a.getByRole("link", { name: "Minha coleção" }).click();
  await expect(a.getByRole("heading", { name: "Minha coleção" })).toBeVisible();
  await expect(a.getByRole("button", { name: new RegExp(`^${list} 1$`) })).toBeVisible();

  // The affiliate refreshes her results and authorizes the mentor.
  await a.getByTitle("Resultados").click();
  // Wait for the sync request to finish: reloading the page earlier cancels it.
  await Promise.all([
    a.waitForResponse((r) => r.request().method() === "POST" && r.url().endsWith("/v1/me/results/sync") && r.ok()),
    a.getByRole("button", { name: "Atualizar agora" }).click(),
  ]);
  await expect(async () => {
    await a.reload();
    await expect(a.getByText(/com os pedidos dos últimos 89 dias/)).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 30_000 });
  await expect(a.getByText("Comissão estimada")).toBeVisible();
  await a.getByLabel(/Mostrar meus resultados ao mentor/).click();
  await expect(a.getByLabel(/Mostrar meus resultados ao mentor/)).toBeChecked();

  // The mentor sees who imported and the cohort with one affiliate sharing.
  await m.reload();
  await expect(m.getByText("1 de 1 afiliado importou")).toBeVisible();
  await m.getByTitle("Resultados").click();
  await m.getByRole("link", { name: "Resultados da turma" }).click();
  await expect(m.getByRole("heading", { name: "Resultados da turma" })).toBeVisible();
  await expect(m.getByText("Autorizam ver resultados").locator("..")).toContainText("1");

  // The mentor subscribes to the seats and simulates the payment.
  await m.getByTitle("Assinatura").click();
  await m.getByLabel("CPF ou CNPJ de quem paga").fill("390.533.447-05");
  await m.getByRole("button", { name: /Assinar por/ }).click();
  await expect(m.getByText("Aguardando o pagamento")).toBeVisible();
  await m.getByRole("button", { name: "Simular pagamento" }).click();
  await expect(m.getByText("Pagamento em dia")).toBeVisible();
  await expect(m.getByText("Assinatura ativa")).toBeVisible();
});
