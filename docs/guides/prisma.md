# Prisma ORM with BranchBase

BranchBase lets a Prisma application keep one stable `DATABASE_URL` while the local proxy routes each connection to the database associated with the active Git branch. That means migrations created on a feature branch can stay isolated from `main` instead of leaving your local database schema in a state that no longer matches the checked-out code.

## Prerequisites

This guide assumes:

- BranchBase is installed and initialized in the repository.
- A local PostgreSQL instance is available to BranchBase.
- Your Prisma project already has a `schema.prisma` file.
- The BranchBase proxy listens on `localhost:5432`, the default shown in the project quickstart.

Initialize BranchBase once from the project root:

```bash
branchbase init
```

Start the transparent proxy before running the application or Prisma commands:

```bash
branchbase proxy
```

## Point Prisma at the BranchBase proxy

Keep Prisma's connection string stable. For example, in `.env`:

```dotenv
DATABASE_URL="postgresql://user:pass@localhost:5432/myapp_dev?schema=public"
```

And reference it from `schema.prisma` as usual:

```prisma
datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}
```

The important part is the proxy host and port. You do not create a different environment variable for every Git branch; BranchBase resolves the active branch and forwards the connection to the matching local branch database.

## Feature-branch workflow

Create or switch to a feature branch with normal Git commands:

```bash
git checkout -b feature/billing
```

Develop the schema and create the migration with Prisma:

```bash
npx prisma migrate dev --name add-billing
npx prisma generate
```

The migration and generated Prisma Client belong to the checked-out source branch. The database changes are applied through the BranchBase proxy to that branch's isolated database rather than forcing `main` to share the same schema state.

You can inspect what BranchBase currently resolves with:

```bash
branchbase status
```

For scripts or editor integrations, use the JSON form:

```bash
branchbase status --json
```

## Switch back to `main`

When you need to leave the feature work, switch branches normally:

```bash
git checkout main
```

With the BranchBase Git hooks installed, the active branch changes while your Prisma `DATABASE_URL` stays untouched. Requests through the proxy are then routed to the `main` branch database, so feature-only migrations such as the new billing schema do not collide with the older `main` code.

If you prefer to inspect or select a target explicitly, BranchBase also exposes:

```bash
branchbase switch main
branchbase status
```

## Prisma Client generation

Database branching and Prisma Client generation solve different problems:

- BranchBase isolates the local database schema and data by Git branch.
- `prisma generate` regenerates the client from the `schema.prisma` currently checked out in the working tree.

Run `npx prisma generate` whenever the checked-out branch changes generated client types or after a migration changes the Prisma schema. Do not rely on a client generated on `feature/billing` after switching back to a branch whose schema is different.

## Recommended routine

A simple day-to-day loop is:

```bash
# once per development session
branchbase proxy

# feature work
git checkout feature/billing
npx prisma migrate dev
npx prisma generate
branchbase status

# return to main
git checkout main
npx prisma generate
branchbase status
```

Keep `.env` stable throughout the workflow. Branch-specific application configuration is still fine when your application genuinely needs it, but the database host, port, and connection variable do not need to change just to follow Git branches.

## Troubleshooting

If Prisma cannot connect, confirm the proxy is running and check the branch resolution:

```bash
branchbase status
```

If Prisma reports generated-client or type mismatches after a checkout, regenerate the client for the current branch:

```bash
npx prisma generate
```

If the checked-out branch introduces a new migration, run the normal Prisma migration command on that branch rather than applying the feature migration to another branch's database:

```bash
npx prisma migrate dev
```

The goal is to keep both halves aligned: Git selects the source tree, and BranchBase selects the corresponding local database branch behind the same Prisma connection string.
