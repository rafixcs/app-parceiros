import { expect, test, type Browser, type Page } from "@playwright/test";

// Roteiro ponta a ponta do MVP, pelas telas: o mentor cria a mentoria, convida
// uma afiliada, publica uma lista; ela importa, recebe o link dela, vê os
// resultados e autoriza o mentor; o mentor vê a turma e paga a assinatura.
// Nomes únicos por execução, para rodar de novo no mesmo banco.

const sufixo = Date.now().toString(36);
const mentor = `mentor-${sufixo}`;
const afiliada = `ana-${sufixo}`;
const mentoria = `Turma ${sufixo}`;
const lista = `Achados ${sufixo}`;

async function entrarComo(browser: Browser, nome: string): Promise<Page> {
  const page = await (await browser.newContext()).newPage();
  await page.goto("/entrar");
  await page.getByLabel("Ambiente local: entrar como").fill(nome);
  await page.getByRole("button", { name: "Entrar" }).click();
  await expect(page.getByRole("heading", { name: "Radar" })).toBeVisible();
  return page;
}

/** Confirma o próximo window.confirm da página. */
function confirmar(page: Page) {
  page.once("dialog", (d) => void d.accept());
}

test("mentor e afiliada percorrem o app do convite à assinatura", async ({ browser }) => {
  // O mentor entra, vê o radar do pessoal e cria a mentoria.
  const m = await entrarComo(browser, mentor);
  // O worker coleta o catálogo (mock) ao subir; espera a primeira coleta.
  await expect(async () => {
    await m.reload();
    await expect(m.getByText(/Atualizado (há|agora)/)).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 60_000 });
  await expect(m.getByRole("button", { name: "Salvar" }).first()).toBeVisible();
  await m.getByTitle("Mentoria").click();
  await m.getByLabel("Nome da mentoria").fill(mentoria);
  await m.getByRole("button", { name: "Criar mentoria" }).click();
  await expect(m.getByRole("heading", { name: mentoria })).toBeVisible();

  // Gera o convite e pega o link.
  await m.getByRole("button", { name: "Gerar convite" }).click();
  const link = await m.locator("input[readonly]").inputValue();
  expect(link).toMatch(/\/convite\/[\w-]+$/);
  await expect(m.getByText("Convites pendentes")).toBeVisible();

  // A afiliada abre o link sem login, entra e aceita.
  const a = await (await browser.newContext()).newPage();
  await a.goto(new URL(link).pathname);
  await expect(a.getByRole("heading", { name: mentoria })).toBeVisible();
  await a.getByRole("button", { name: "Entrar para aceitar" }).click();
  await a.getByLabel("Ambiente local: entrar como").fill(afiliada);
  await a.getByRole("button", { name: "Entrar" }).click();
  await a.getByRole("button", { name: "Aceitar convite" }).click();
  await expect(a.getByRole("heading", { name: "Listas" })).toBeVisible();
  await expect(a.getByText("O seu mentor ainda não publicou listas.")).toBeVisible();

  // O convite usado não vale de novo.
  const outro = await (await browser.newContext()).newPage();
  await outro.goto(new URL(link).pathname);
  await expect(outro.getByText("Este convite já foi usado.")).toBeVisible();

  // O mentor vê a afiliada na turma.
  await m.reload();
  await expect(m.getByText(`${afiliada}@dev.local`)).toBeVisible();

  // A afiliada conecta a conta Shopee (mock).
  await a.getByRole("link", { name: "Shopee" }).click();
  await a.getByLabel("AppID").fill("18300001234");
  await a.getByLabel("Secret").fill("segredo-de-teste");
  await a.getByRole("button", { name: "Conectar", exact: true }).click();
  await expect(a.getByText("Conectada", { exact: true })).toBeVisible();
  await expect(a.getByText(/AppID ••••1234/)).toBeVisible();

  // O mentor monta a lista com um produto do radar e publica.
  await m.getByTitle("Listas").click();
  await m.getByRole("button", { name: "Nova lista" }).click();
  await m.getByLabel("Título").fill(lista);
  await m.getByRole("button", { name: "Criar e adicionar produtos" }).click();
  await expect(m.getByRole("heading", { name: lista })).toBeVisible();
  await expect(m.getByText("Rascunho", { exact: true })).toBeVisible();
  await m.getByLabel("Adicionar produto").fill("fone");
  await m.getByRole("button", { name: "Adicionar" }).first().click();
  await m.getByLabel("Dica para a turma").fill("Vende muito no reels");
  await m.getByLabel("Dica para a turma").blur();
  await expect(m.getByText("Salvando…")).toHaveCount(0);
  confirmar(m);
  await m.getByRole("button", { name: "Publicar para a turma" }).click();
  await expect(m.getByText("Lista publicada. A turma está sendo avisada.")).toBeVisible();

  // A afiliada recebe o aviso e importa a lista com o link dela.
  await a.goto("/");
  await expect(a.getByRole("link", { name: /Notificações \(1 não lidas\)/ })).toBeVisible();
  await a.getByRole("link", { name: /Notificações/ }).click();
  await a.getByText(`Nova lista: ${lista}`).click();
  await expect(a.getByRole("heading", { name: lista })).toBeVisible();
  await a.getByRole("button", { name: "Importar a lista para a minha coleção" }).click();
  await expect(a.getByRole("button", { name: "Tudo já está na sua coleção" })).toBeVisible();
  await expect(async () => {
    await a.reload();
    await expect(a.getByText("Link pronto")).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 30_000 });

  // Na coleção, o item tem a dica do mentor e um link por canal.
  await a.getByRole("link", { name: "Na sua coleção" }).click();
  await expect(a.getByRole("heading", { name: "Link de afiliado" })).toBeVisible();
  await expect(a.getByLabel("Notas (só você vê)")).toHaveValue("Dica do mentor: Vende muito no reels");
  await expect(a.getByRole("button", { name: /Copiar link do Instagram/ })).toBeVisible();
  await a.getByRole("link", { name: "Minha coleção" }).click();
  await expect(a.getByRole("heading", { name: "Minha coleção" })).toBeVisible();
  await expect(a.getByRole("button", { name: new RegExp(`^${lista} 1$`) })).toBeVisible();

  // A afiliada atualiza os resultados e autoriza o mentor.
  await a.getByTitle("Resultados").click();
  await a.getByRole("button", { name: "Atualizar agora" }).click();
  await expect(async () => {
    await a.reload();
    await expect(a.getByText(/com os pedidos dos últimos 89 dias/)).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 30_000 });
  await expect(a.getByText("Comissão estimada")).toBeVisible();
  await a.getByLabel(/Mostrar meus resultados ao mentor/).click();
  await expect(a.getByLabel(/Mostrar meus resultados ao mentor/)).toBeChecked();

  // O mentor vê quem importou e a turma com uma afiliada autorizando.
  await m.reload();
  await expect(m.getByText("1 de 1 afiliado importou")).toBeVisible();
  await m.getByTitle("Resultados").click();
  await m.getByRole("link", { name: "Resultados da turma" }).click();
  await expect(m.getByRole("heading", { name: "Resultados da turma" })).toBeVisible();
  await expect(m.getByText("Autorizam ver resultados").locator("..")).toContainText("1");

  // O mentor contrata os assentos e simula o pagamento.
  await m.getByTitle("Assinatura").click();
  await m.getByLabel("CPF ou CNPJ de quem paga").fill("390.533.447-05");
  await m.getByRole("button", { name: /Assinar por/ }).click();
  await expect(m.getByText("Aguardando o pagamento")).toBeVisible();
  await m.getByRole("button", { name: "Simular pagamento" }).click();
  await expect(m.getByText("Pagamento em dia")).toBeVisible();
  await expect(m.getByText("Assinatura ativa")).toBeVisible();
});
