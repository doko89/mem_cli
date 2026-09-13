# PRD — mem_cli

## 1. Overview

`mem_cli` adalah **local-first persistent memory layer untuk AI agents**.

`mem_cli` menyediakan storage dan retrieval memory lintas sesi menggunakan SQLite dan CLI yang sederhana, deterministic, serta machine-readable.

Core model:

```text
Namespace
    ↓
Subject
    ↓
Memory
    ├── type
    ├── content
    ├── metadata
    └── source
```

Contoh:

```text
namespace: work/infra
subject: topology
type: fact

content:
Production Kubernetes cluster menggunakan 3 control-plane nodes.
Worker nodes tersebar di 3 availability zones.
Cilium digunakan sebagai CNI.
```

`mem_cli` ditujukan terutama untuk digunakan oleh AI agents, tetapi tetap nyaman digunakan secara manual oleh developer.

---

# 2. Goals

### Primary Goals

1. Menyediakan persistent memory lintas session untuk AI agents.
2. Menyimpan memory secara local-first menggunakan SQLite.
3. Mengorganisasi memory berdasarkan namespace dan subject.
4. Menyediakan full-text search yang cepat menggunakan SQLite FTS5.
5. Menghasilkan search result yang ringkas melalui dynamic snippet.
6. Memungkinkan agent mengambil memory lengkap ketika diperlukan.
7. Menyediakan CLI dengan JSON output yang stabil.
8. Menyediakan namespace hierarchy yang predictable.
9. Menjadi foundation yang nantinya dapat digunakan oleh CLI maupun MCP.
10. Menjaga core memory engine independen dari interface.

### Secondary Goals

* Idempotent namespace creation.
* Namespace normalization.
* Memory expiration.
* Tags dan metadata.
* Import/export memory.
* Deterministic retrieval.
* Source/provenance untuk imported memory.

---

# 3. Non-Goals

Fitur berikut tidak termasuk MVP:

* Vector database.
* Embedding generation.
* Semantic/vector search.
* LLM-based memory extraction.
* LLM-generated summaries.
* Automatic conflict resolution.
* Knowledge graph.
* Entity graph.
* Cloud synchronization.
* Multi-user synchronization.
* Authentication.
* Authorization subsystem.
* Web UI.
* Distributed storage.

---

# 4. Product Architecture

`mem_cli` bukan keseluruhan memory engine.

Arsitektur:

```text
                  ┌─────────────────────┐
                  │    Memory Engine    │
                  │                     │
                  │ Namespace           │
                  │ Memory              │
                  │ Retrieval           │
                  │ Import / Export     │
                  └──────────┬──────────┘
                             │
                    ┌────────┴────────┐
                    │                 │
                   CLI              MCP
                  (MVP)            (future)
                    │                 │
                    └────────┬────────┘
                             │
                           SQLite
```

MVP hanya mengimplementasikan CLI.

Namun seluruh business logic harus berada pada memory engine sehingga MCP nantinya hanya menjadi adapter/interface tambahan.

---

# 5. Core Concepts

## 5.1 Namespace

Namespace adalah **logical domain/container** tempat memory berada.

Contoh:

```text
work/infra
work/development
personal
```

Namespace dapat memiliki hierarchy:

```text
work/
├── infra/
│   ├── kubernetes/
│   └── networking/
└── development/
    ├── backend/
    └── frontend/
```

Namespace menjawab:

> Memory ini berada di domain mana?

---

## 5.2 Subject

Subject adalah **topik yang dibahas oleh memory**.

Contoh:

```text
topology
networking
architecture
database
deployment
business-rules
```

Subject menjawab:

> Memory ini membahas apa?

Namespace dan subject memiliki fungsi berbeda:

```text
namespace = where/domain
subject   = what/topic
content   = knowledge
```

Contoh:

```text
namespace: work/infra
subject: topology
content: Production Kubernetes menggunakan 3 control-plane nodes.
```

---

## 5.3 Memory Type

Type menjelaskan jenis memory.

MVP:

```text
fact
decision
preference
todo
entity
```

Contoh:

```text
fact
```

Informasi yang diketahui/dianggap benar.

```text
decision
```

Keputusan yang telah dibuat.

```text
preference
```

Preferensi.

```text
todo
```

Hal yang perlu dilakukan.

```text
entity
```

Informasi mengenai entity.

Subject dan type tidak boleh dianggap sebagai konsep yang sama.

---

# 6. Memory Model

Memory memiliki struktur:

```text
Memory
├── id
├── namespace_id
├── subject
├── type
├── content
├── reason
├── tags
├── metadata
├── source
├── created_at
├── updated_at
└── expires_at
```

### `content`

Isi knowledge yang sebenarnya.

`content` adalah canonical source of truth.

### `reason`

Alasan memory disimpan, terutama berguna untuk `decision`, `preference`, atau memory yang membutuhkan context tambahan.

### `tags`

Optional labels untuk filtering.

### `metadata`

Optional structured metadata.

### `source`

Informasi asal memory.

Contoh:

```text
source.type = file
source.path = docs/topology.md
```

### `expires_at`

Optional expiration time.

Memory yang telah expired tidak muncul pada normal retrieval.

---

# 7. Dynamic Search Snippet

`snippet` **bukan field yang disimpan di memory**.

Snippet merupakan potongan `content` yang dihasilkan secara dinamis berdasarkan query.

Contoh content:

```text
Production Kubernetes cluster menggunakan 3 control-plane nodes.
Worker nodes tersebar di 3 availability zones.
Cilium digunakan sebagai CNI.
Ingress menggunakan nginx.
Database tidak berjalan di dalam cluster.
```

Query:

```bash
mem search \
  --namespace work/infra \
  --query "availability zones"
```

Hasil:

```json
{
  "id": "mem_123",
  "subject": "topology",
  "type": "fact",
  "snippet": "Worker nodes tersebar di 3 availability zones.",
  "score": 0.91
}
```

Snippet dihasilkan dari FTS5 berdasarkan bagian content yang cocok dengan query.

### Prinsip

```text
content
   ↓
FTS5
   ↓
query match
   ↓
dynamic snippet
```

Tidak ada data snippet yang perlu disimpan.

Keuntungannya:

* tidak ada duplicate data;
* snippet selalu sesuai dengan content terbaru;
* tidak perlu maintenance;
* deterministic;
* cepat;
* tidak membutuhkan LLM.

---

# 8. Search vs Get

`mem search` dan `mem get` memiliki tujuan berbeda.

## Search

Tujuan:

> Menemukan memory yang relevan.

```bash
mem search \
  --namespace work/infra \
  --query "availability zones"
```

Hasil mengutamakan:

```text
id
subject
type
snippet
score
```

Bukan seluruh content.

## Get

Tujuan:

> Mengambil memory secara lengkap.

```bash
mem get mem_123
```

Mengembalikan full content dan metadata.

Dengan demikian agent dapat melakukan:

```text
search
  ↓
lihat snippet
  ↓
memory relevan?
  ↓
get
  ↓
baca full content
```

Hal ini mengurangi context/token usage agent.

---

# 9. Namespace Hierarchy

Namespace menggunakan path-based hierarchy.

Contoh:

```text
work/infra/kubernetes
```

berarti:

```text
work
└── infra
    └── kubernetes
```

Parent ditentukan dari path.

Tidak diperlukan:

```bash
--parent
```

karena path sudah menjadi source of truth.

---

# 10. Namespace Creation

Command:

```bash
mem ns create <path>
```

Semantics mengikuti `mkdir -p`.

Contoh:

```bash
mem ns create work/infra/kubernetes
```

Jika belum ada:

```text
work
└── infra
    └── kubernetes
```

dibuat dalam satu transaction.

Jika `work` dan `work/infra` sudah ada, hanya `work/infra/kubernetes` yang dibuat.

Jika semuanya sudah ada, command bersifat idempotent.

Contoh result pertama:

```json
{
  "ok": true,
  "command": "ns.create",
  "data": {
    "id": "ns_123",
    "name": "work/infra",
    "created": true
  }
}
```

Pemanggilan kedua:

```json
{
  "ok": true,
  "command": "ns.create",
  "data": {
    "id": "ns_123",
    "name": "work/infra",
    "created": false
  }
}
```

---

# 11. Namespace Normalization

Namespace harus dinormalisasi sebelum dibandingkan.

Contoh:

```text
Work/Infra
work//infra
work/infra/
./work/infra
```

harus menghasilkan canonical form:

```text
work/infra
```

Database menyimpan:

```text
name
normalized_name
```

dengan constraint:

```sql
UNIQUE(normalized_name)
```

Tujuannya mencegah duplicate akibat formatting yang berbeda.

---

# 12. Namespace Typo Detection

Exact identity ditentukan oleh canonical namespace.

Contoh:

```text
work/infra
```

dan:

```text
works/infra
```

tidak dianggap sama.

`mem_cli` **tidak boleh melakukan fuzzy auto-merge**.

Namun CLI dapat mendeteksi kemungkinan typo dan memberikan candidate:

```json
{
  "ok": false,
  "error": {
    "code": "POSSIBLE_DUPLICATE_NAMESPACE",
    "message": "A similar namespace already exists.",
    "candidates": [
      "work/infra"
    ]
  }
}
```

Fuzzy matching hanya digunakan sebagai warning/candidate discovery.

Canonical identity tetap berdasarkan normalized path.

---

# 13. Agent Namespace Convention

`mem_cli` tidak memiliki authorization subsystem pada MVP.

Pembagian namespace dilakukan melalui persona atau agent configuration.

Contoh:

```text
Infra Agent
namespace: work/infra
```

```text
Development Agent
namespace: work/development
```

Persona dapat memiliki rule:

```text
Use namespace work/infra for persistent memory.
```

atau:

```text
Use namespace work/development for persistent memory.
```

`mem_cli` tidak perlu mengetahui identity agent.

Tanggung jawab:

```text
Agent / Persona
    ↓
memilih namespace

mem_cli
    ↓
menyimpan dan mengambil memory
```

---

# 14. CLI

## 14.1 Namespace Commands

Create:

```bash
mem ns create <path>
```

List:

```bash
mem ns list
```

Optional path:

```bash
mem ns list work
```

Get:

```bash
mem ns get <id|path>
```

Delete:

```bash
mem ns delete <id|path>
```

Deletion semantics harus eksplisit terhadap child namespace dan associated memories.

MVP tidak boleh melakukan recursive destructive deletion secara diam-diam.

---

# 15. Memory Commands

## 15.1 Add

```bash
mem add \
  --namespace work/infra \
  --subject topology \
  --type fact \
  --content "Production Kubernetes cluster menggunakan 3 control-plane nodes."
```

Optional:

```text
--reason
--tags
--source
--expires-at
--metadata
```

---

## 15.2 Get

```bash
mem get <memory-id>
```

Mengembalikan full memory.

---

## 15.3 List

```bash
mem list --namespace work/infra
```

Filter:

```bash
--subject topology
--type fact
--tag kubernetes
```

---

## 15.4 Search

```bash
mem search \
  --namespace work/infra \
  --query "production kubernetes"
```

Search dilakukan terhadap:

```text
subject
content
reason
```

dan hasil menampilkan dynamic snippet.

---

## 15.5 Update

```bash
mem update <memory-id> ...
```

Field yang mutable harus ditentukan secara eksplisit.

Identity fields seperti:

```text
id
created_at
```

tidak boleh berubah.

Content-related fields dapat diperbarui.

---

## 15.6 Forget

```bash
mem forget <memory-id>
```

MVP menggunakan hard delete jika tidak ada requirement audit/history.

Soft-delete tidak diperlukan pada MVP.

---

# 16. Context

`mem context` adalah interface retrieval tingkat tinggi untuk agent.

Contoh:

```bash
mem context \
  --namespace work/infra \
  --query "bagaimana topology production?"
```

Optional:

```text
--subject topology
--type fact
--limit 10
```

Context mengembalikan memory yang paling relevan, tetapi hasil dapat menggunakan dynamic snippet agar tidak memasukkan content panjang yang tidak diperlukan.

Contoh:

```json
{
  "memories": [
    {
      "id": "mem_123",
      "subject": "topology",
      "type": "fact",
      "snippet": "Production menggunakan 3 control-plane nodes dan worker tersebar di 3 availability zones."
    },
    {
      "id": "mem_456",
      "subject": "networking",
      "type": "fact",
      "snippet": "Cilium digunakan sebagai CNI untuk production cluster."
    }
  ]
}
```

Agent dapat melakukan `mem get` jika membutuhkan detail lengkap.

---

# 17. Retrieval

Retrieval MVP menggunakan:

1. Namespace filtering.
2. Subject filtering.
3. Type filtering.
4. Tag filtering.
5. SQLite FTS5.
6. Relevance ranking.
7. Recency.
8. Expiration filtering.

Memory dengan:

```text
expires_at < now
```

tidak muncul dalam normal `search` atau `context`.

---

# 18. FTS5

SQLite FTS5 digunakan sebagai full-text retrieval engine.

Searchable fields:

```text
subject
content
reason
```

Namespace tidak perlu menjadi bagian full-text search.

Namespace filtering dilakukan melalui relational query.

FTS5 digunakan untuk:

* matching query;
* ranking;
* dynamic snippet generation.

---

# 19. Dynamic Snippet Rules

Snippet harus:

* berasal dari `content`;
* relevan terhadap query;
* memiliki panjang terbatas;
* menunjukkan bagian yang menyebabkan memory match;
* tidak mengubah content;
* tidak membutuhkan LLM.

Contoh:

```text
Content:
Production menggunakan 3 control-plane nodes.
Worker tersebar di 3 availability zones.
Cilium digunakan sebagai CNI.
```

Query:

```text
availability zones
```

Snippet:

```text
Worker tersebar di 3 availability zones.
```

Query:

```text
Cilium
```

Snippet:

```text
Cilium digunakan sebagai CNI.
```

Snippet bukan canonical data.

---

# 20. Import

Import digunakan untuk memasukkan content eksternal ke memory.

Command:

```bash
mem import <file>
```

Contoh:

```bash
mem import docs/topology.md \
  --namespace work/infra \
  --subject topology
```

Default behavior untuk plain Markdown:

> Satu file menjadi satu memory.

Dengan demikian `mem_cli` tidak secara otomatis memecah setiap paragraph atau heading menjadi memory berbeda.

Hasil:

```text
namespace: work/infra
subject: topology
type: fact
content: <seluruh isi file>
source:
  type: file
  path: docs/topology.md
```

Import tidak menggunakan LLM pada MVP.

---

# 21. Import vs Add

`mem add` digunakan untuk satu memory:

```bash
mem add ...
```

`mem import` digunakan untuk memasukkan content dari external source:

```bash
mem import file.md ...
```

Tidak menggunakan:

```bash
mem add --import=file.md
```

karena `add` dan `import` memiliki semantic yang berbeda.

---

# 22. Export

Untuk round-trip dan backup sederhana:

```bash
mem export --namespace work/infra
```

Export dapat menghasilkan Markdown atau JSON.

Contoh:

```bash
mem export \
  --namespace work/infra \
  --format markdown
```

Source/provenance tetap dipertahankan bila tersedia.

---

# 23. Source / Provenance

Memory yang berasal dari file sebaiknya menyimpan source.

Contoh:

```json
{
  "source": {
    "type": "file",
    "path": "docs/topology.md"
  }
}
```

Source bukan bagian dari content.

Tujuannya:

* mengetahui asal memory;
* memudahkan debugging;
* mendukung import/export;
* memungkinkan provenance di masa depan.

---

# 24. Tags dan Metadata

MVP menggunakan JSON/TEXT untuk tags dan metadata.

Contoh tags:

```json
[
  "kubernetes",
  "production"
]
```

Metadata:

```json
{
  "cluster": "production",
  "region": "ap-southeast-1"
}
```

Tidak perlu relational tag system pada MVP.

---

# 25. SQLite Schema

Namespace:

```sql
CREATE TABLE namespaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL UNIQUE,
    parent_id TEXT,
    description TEXT,
    created_at TEXT NOT NULL,

    FOREIGN KEY (parent_id)
        REFERENCES namespaces(id)
);
```

Memory:

```sql
CREATE TABLE memories (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    type TEXT NOT NULL,
    content TEXT NOT NULL,
    reason TEXT,
    tags TEXT,
    metadata TEXT,
    source TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    expires_at TEXT,

    FOREIGN KEY (namespace_id)
        REFERENCES namespaces(id)
);
```

SQLite harus menggunakan:

```sql
PRAGMA foreign_keys = ON;
```

Namespace creation harus transactional.

---

# 26. JSON Output Contract

Semua command harus dapat menghasilkan JSON machine-readable.

Success:

```json
{
  "ok": true,
  "command": "mem.add",
  "data": {},
  "meta": {
    "schema_version": 1
  }
}
```

Error:

```json
{
  "ok": false,
  "error": {
    "code": "NAMESPACE_NOT_FOUND",
    "message": "Namespace 'work/infra' does not exist."
  },
  "meta": {
    "schema_version": 1
  }
}
```

Schema version harus selalu tersedia.

---

# 27. Error Codes

Minimal:

```text
INVALID_ARGUMENT
INVALID_NAMESPACE
NAMESPACE_NOT_FOUND
NAMESPACE_CONFLICT
POSSIBLE_DUPLICATE_NAMESPACE
MEMORY_NOT_FOUND
INVALID_MEMORY_TYPE
DATABASE_ERROR
IMPORT_ERROR
```

`code` bersifat machine-readable dan harus stabil.

`message` bersifat human-readable.

---

# 28. Exit Codes

CLI juga menggunakan process exit codes.

Minimal:

```text
0  success
1  general error
2  invalid argument
3  not found
4  conflict
```

Exact mapping ditetapkan pada implementation specification.

JSON error dan exit code harus konsisten.

---

# 29. Idempotency

Namespace creation wajib idempotent.

```bash
mem ns create work/infra
```

tidak boleh menghasilkan:

```text
work/infra
work/infra-2
work/infra-3
```

Unique constraint pada:

```text
normalized_name
```

menjadi final database guarantee.

---

# 30. Concurrency

SQLite menjadi single source of truth.

Concurrent operations harus aman terhadap duplicate namespace creation.

Contoh dua proses:

```text
Process A → create work/infra
Process B → create work/infra
```

hasil akhirnya harus tetap:

```text
work/infra
```

hanya satu namespace.

Transaction dan database constraint harus menjadi mekanisme enforcement.

---

# 31. Performance Requirements

Target MVP untuk database hingga sekitar 100.000 memories:

```text
CRUD        < 50 ms
namespace   indexed lookup
search      FTS5
startup     low-latency
```

CLI harus cukup cepat untuk dipanggil berkali-kali oleh agent.

Tidak diperlukan daemon untuk operasi normal.

---

# 32. CLI Design Principles

CLI harus:

* predictable;
* deterministic;
* machine-readable;
* composable;
* mudah dipanggil agent;
* mudah digunakan developer;
* tidak menghasilkan conversational prose secara default;
* memiliki stable error codes;
* tidak melakukan destructive action secara diam-diam.

Human-readable output tetap diperbolehkan.

JSON output harus tersedia secara eksplisit atau menjadi default berdasarkan keputusan implementasi final.

---

# 33. Technology

Rekomendasi MVP:

```text
Language: Go
Database: SQLite
Search: SQLite FTS5
CLI: Cobra
```

Alasan:

* single binary;
* deployment sederhana;
* startup cepat;
* cocok untuk local-first;
* cocok dipanggil oleh AI agents;
* SQLite integration matang;
* tidak membutuhkan runtime tambahan.

---

# 34. MVP Scope

## F1 — Core

* [ ] SQLite storage.
* [ ] Namespace CRUD.
* [ ] Namespace hierarchy.
* [ ] Namespace normalization.
* [ ] Idempotent namespace creation.
* [ ] Memory CRUD.
* [ ] Subject.
* [ ] Memory type.
* [ ] Tags.
* [ ] Metadata.
* [ ] Expiration.
* [ ] Source/provenance.
* [ ] JSON output.
* [ ] Stable error codes.
* [ ] Exit codes.

## F2 — Retrieval

* [ ] FTS5.
* [ ] Namespace filtering.
* [ ] Subject filtering.
* [ ] Type filtering.
* [ ] Tag filtering.
* [ ] Relevance ranking.
* [ ] Recency ranking.
* [ ] Dynamic snippet generation.
* [ ] Deterministic ranking.
* [ ] `mem context`.

## F3 — File Operations

* [ ] `mem import`.
* [ ] Markdown import.
* [ ] File source tracking.
* [ ] `mem export`.
* [ ] JSON export.
* [ ] Markdown export.

---

# 35. Explicitly Excluded From MVP

* [ ] Vector search.
* [ ] Embeddings.
* [ ] LLM-generated snippets.
* [ ] LLM-generated summaries.
* [ ] Automatic memory extraction.
* [ ] Semantic deduplication.
* [ ] Conflict detection.
* [ ] Memory supersession.
* [ ] Knowledge graph.
* [ ] Authentication.
* [ ] Authorization.
* [ ] Multi-user sync.
* [ ] Cloud sync.
* [ ] Web UI.
* [ ] MCP server.

---

# 36. Future Roadmap

## Phase 2 — MCP

Setelah memory engine dan CLI stabil:

```text
Memory Engine
    ├── CLI
    └── MCP Server
```

MCP hanya menjadi adapter terhadap memory engine.

Potential tools:

```text
memory_add
memory_get
memory_search
memory_context
memory_list
memory_forget
```

Tidak boleh ada business logic berbeda antara CLI dan MCP.

---

## Phase 3 — Semantic Retrieval

Menambahkan:

* embeddings;
* vector search;
* hybrid FTS + vector search;
* semantic similarity.

Architecture:

```text
Query
  │
  ├── FTS5
  │
  └── Vector Search
        │
        ▼
    Hybrid Ranking
```

---

## Phase 4 — Memory Quality

Potential features:

* duplicate detection;
* conflict detection;
* memory supersession;
* provenance;
* confidence;
* historical memory.

Contoh:

```text
Memory A
   │
   └── superseded_by
          ↓
       Memory B
```

Memory lama tidak harus langsung dihapus.

---

## Phase 5 — Sync and Security

Jika diperlukan:

* remote sync;
* authentication;
* authorization;
* multi-user;
* multi-machine;
* conflict resolution.

Fitur-fitur tersebut sengaja tidak menjadi bagian MVP.

---

# 37. Example Workflow

## Agent menyimpan memory

```bash
mem add \
  --namespace work/infra \
  --subject topology \
  --type fact \
  --content "Production Kubernetes cluster menggunakan 3 control-plane nodes. Worker nodes tersebar di 3 availability zones."
```

## Agent mencari

```bash
mem search \
  --namespace work/infra \
  --query "availability zones"
```

Hasil:

```text
mem_123
subject: topology
type: fact

Worker nodes tersebar di 3 availability zones.
```

## Agent membutuhkan detail

```bash
mem get mem_123
```

Hasil:

```text
Production Kubernetes cluster menggunakan 3 control-plane nodes.
Worker nodes tersebar di 3 availability zones.
```

## Agent mengimpor dokumentasi

```bash
mem import docs/topology.md \
  --namespace work/infra \
  --subject topology
```

Seluruh file menjadi satu memory.

Search tetap dapat menemukan bagian tertentu melalui FTS5 dynamic snippet.

---

# 38. Final Mental Model

`mem_cli` bukan document database dan bukan knowledge graph.

Ia adalah:

> **Persistent, local-first memory store dengan namespace, subject, full-text retrieval, dan context-oriented results untuk AI agents.**

Model sederhananya:

```text
                         Agent
                           │
                           ▼
                         mem
                           │
                  ┌────────┴────────┐
                  │                 │
               Namespace          Memory
                  │                 │
             work/infra             ├── subject
                                    ├── type
                                    ├── content
                                    ├── metadata
                                    └── source
                                      │
                                      ▼
                                     FTS5
                                      │
                         ┌────────────┴────────────┐
                         │                         │
                       Search                    Context
                         │                         │
                         ▼                         ▼
                    dynamic snippet          dynamic snippet
                         │                         │
                         └────────────┬────────────┘
                                      │
                                      ▼
                                    Agent
                                      │
                              needs full content?
                                      │
                                      ▼
                                  mem get
```

Prinsip fundamental:

1. **Namespace menentukan domain.**
2. **Subject menentukan topik.**
3. **Content adalah canonical knowledge.**
4. **Snippet bukan data; snippet adalah hasil retrieval.**
5. **FTS5 menjadi fondasi search MVP.**
6. **Search mengembalikan informasi ringkas; `get` mengembalikan detail lengkap.**
7. **Import tidak otomatis memecah dokumen menjadi banyak memory.**
8. **Agent/persona menentukan namespace yang digunakan.**
9. **CLI adalah interface MVP; memory engine harus MCP-ready.**
10. **Jangan menambahkan semantic/LLM complexity sebelum core retrieval terbukti berguna.**
