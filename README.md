# App Parceiros

App para afiliados da Shopee descobrirem produtos em alta, organizarem o que vão divulgar (link próprio, notas, título e descrição), verem quanto ganham por venda e acompanharem resultados, com curadoria de um mentor para a sua turma.

- [Especificação do MVP](docs/mvp.md)
- [Stack](docs/stack.md)
- [Pesquisa de mercado e viabilidade](docs/pesquisa.md)

## Rodando localmente

Pré-requisitos: Docker, [kind](https://kind.sigs.k8s.io/), [Tilt](https://tilt.dev/) e Go 1.26.

```sh
make cluster   # uma vez
tilt up        # sobe tudo; API em http://localhost:8080/readyz
```

Sem Kubernetes, com Postgres e Redis já rodando:

```sh
export DATABASE_URL=postgres://... REDIS_URL=redis://...
make migrate
./bin/parceiros api      # ou: ./bin/parceiros worker
```
