# mem

`mem` adalah local-first persistent memory layer untuk AI agents. Data disimpan dalam satu database SQLite dengan FTS5, dikemas sebagai binary CLI tunggal, dan mengembalikan output JSON yang mudah dipakai program lain.

Proyek ini tanpa cloud, tanpa embedding, dan tanpa MCP.

## Fitur

- Namespace hierarkis dan memory berbasis subject.
- Full-text search menggunakan SQLite FTS5.
- Relasi antar-memory (`--related`) dengan backlink dua arah.
- Konten multi-line atau dokumen panjang secara verbatim.
- Import satu file sebagai satu memory.
- Riwayat versi otomatis untuk update dan forget.
- Export JSON atau Markdown.
- Output JSON konsisten dengan envelope `{"ok", "command", "data", "meta": {"schema_version"}}`.

## Build

Butuh Go 1.26 atau yang lebih baru:

```bash
go build -o mem .
./mem --help
```

Secara opsional, salin binary ke direktori `PATH`:

```bash
install -m 0755 mem /usr/local/bin/mem
```

Driver SQLite yang dipakai adalah `modernc.org/sqlite`, sehingga tidak memerlukan instalasi SQLite terpisah.

## Database

Database default berada di:

```text
$HOME/.mem_cli/mem.db
```

Gunakan `--db` untuk memilih lokasi lain:

```bash
mem --db ./mem.db list --namespace work
```

Saat membuka database schema versi 1, CLI melakukan migrasi idempoten ke versi 2. Backup otomatis dibuat sebagai `mem.db.bak-<timestamp>` sebelum migrasi.

## Tipe memory

```text
fact | decision | preference | todo | entity | doc
```

`mem add` default ke `fact`. `mem import` default ke `doc` karena hasil import biasanya berupa dokumen referensi.

`list`, `search`, `context`, dan `export` tidak memfilter tipe secara default, sehingga semua enam tipe ikut ditampilkan. Gunakan `--type` untuk mempersempit hasil.

## Contoh penggunaan

Buat namespace:

```bash
mem ns create work/infra
```

Tambah memory:

```bash
mem add \
  --namespace work/infra \
  --subject topology \
  --type fact \
  --content $'Production cluster uses 3 control-plane nodes.\nWorkers are spread across 3 zones.'
```

Ambil satu memory lengkap; prefix ID unik juga didukung:

```bash
mem get mem_c46c
```

Secara default memory yang sudah kadaluwarsa disembunyikan. Untuk audit, akses eksplisit by ID dapat memakai:

```bash
mem get mem_c46c --include-expired
```

Cari dengan FTS5:

```bash
mem search \
  --namespace work/infra \
  --query "control-plane nodes" \
  --limit 10
```

FTS5 mendukung wildcard akhiran. Query berikut mencocokkan `control-plane`, `controls`, atau kata berawalan `control` lain:

```bash
mem search --namespace work/infra --query "control*"
```

Wildcard hanya valid di akhir term; penggunaan lain mengembalikan `INVALID_ARGUMENT`.

Untuk konteks agent, gunakan mode pencarian longgar:

```bash
mem context \
  --namespace work/infra \
  --query "kubernetes topology" \
  --limit 10
```

Tampilkan ringkasan memory:

```bash
mem list --namespace work/infra --limit 100
```

Filter berdasarkan subject, tipe, tag, atau relasi:

```bash
mem list --namespace work/infra --type doc
mem list --namespace work/infra --tag production
mem list --namespace work/infra --related-to mem_c46c
```

Update memory:

```bash
mem update mem_c46c --content "Topology has changed."
mem update mem_c46c --clear-expiry
```

Hapus memory:

```bash
mem forget mem_c46c
```

## File multi-line dan import

`--file` menyimpan isi file apa adanya, termasuk newline, heading, dan code fence. Nilai `-` membaca stdin:

```bash
mem add \
  --namespace work/infra \
  --subject runbook \
  --file docs/runbook.md

cat notes.md | mem add \
  --namespace work/infra \
  --subject notes \
  --file -
```

Import satu file menjadi satu memory:

```bash
mem import docs/runbook.md --namespace work/infra
```

Perilaku import:

- `content` berisi file secara byte-per-byte.
- `source.path` otomatis diisi path file.
- Subject default adalah nama file tanpa ekstensi.
- Tipe default adalah `doc`.
- Subject dan tipe tetap bisa dioverride.

```bash
mem import docs/sop-deployment.md \
  --namespace work/infra \
  --subject "deployment SOP" \
  --metadata '{"format":"markdown"}'
```

## Relasi dan backlink

Nilai `--related` dapat berupa:

1. ID memory lengkap.
2. Prefix ID yang unik.
3. Subject persis yang unik di namespace memory sumber.

Prioritas resolusinya adalah exact ID, prefix ID unik, lalu subject unik. Jika subject ambigu, CLI mengembalikan `AMBIGUOUS_SUBJECT` beserta daftar kandidatnya.

```bash
memory_id=$(mem add \
  --namespace work/infra \
  --subject topology \
  --content "Cluster topology detail." | jq -r '.data.id')

mem add \
  --namespace work/infra \
  --subject incident-notes \
  --type doc \
  --content "Related incident notes." \
  --related "$memory_id" \
  --relation related
```

Perintah berikut menampilkan link keluar di `related_out` dan link masuk di `backlinks`:

```bash
mem get "$memory_id"
```

`--relation` memberi label pada relasi, misalnya `depends-on`, `supersedes`, atau `related`.

## Riwayat versi

Setiap update sukses menyimpan snapshot state lama secara otomatis. Operasi `forget` juga menyimpan snapshot terakhir sebelum memory dihapus.

Lihat daftar versi:

```bash
mem history mem_c46c
```

Lihat snapshot penuh versi tertentu:

```bash
mem history mem_c46c --version 1
```

Restore versi lama sebagai operasi update biasa:

```bash
mem revert mem_c46c --version 1
```

## Export

Export JSON:

```bash
mem export --namespace work/infra --format json
```

Export Markdown:

```bash
mem export --namespace work/infra --format markdown > memories.md
```

## Output JSON

Response sukses mengikuti pola ini:

```json
{
  "ok": true,
  "command": "add",
  "data": {},
  "meta": {
    "schema_version": 2
  }
}
```

Response error juga JSON dan berisi code yang stabil:

```json
{
  "ok": false,
  "command": "add",
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "subject is required"
  },
  "meta": {
    "schema_version": 2
  }
}
```

Contoh error code antara lain:

- `INVALID_ARGUMENT`
- `INVALID_NAMESPACE`
- `NAMESPACE_NOT_FOUND`
- `MEMORY_NOT_FOUND`
- `AMBIGUOUS_ID`
- `AMBIGUOUS_SUBJECT`
- `INVALID_MEMORY_TYPE`
- `DATABASE_ERROR`

## Referensi command

| Command | Fungsi |
|---|---|
| `mem ns create/list/get/delete` | Mengelola namespace |
| `mem add` | Menambah memory |
| `mem get` | Mengambil satu memory lengkap |
| `mem list` | Menampilkan ringkasan memory |
| `mem search` | Full-text search FTS5 |
| `mem context` | Pencarian konteks dengan mode `any` |
| `mem update` | Mengubah field mutable |
| `mem forget` | Menghapus memory |
| `mem import` | Mengimport satu file sebagai satu memory |
| `mem export` | Export namespace ke JSON atau Markdown |
| `mem history` | Melihat atau menampilkan versi memory |
| `mem revert` | Restore versi lama |

Gunakan `mem <command> --help` untuk daftar flag lengkap.

## Pengembangan

Jalankan seluruh test:

```bash
go test ./...
```

Jalankan vet dan build:

```bash
go vet ./...
go build -o mem .
```

## Desain

Detail produk dan skema database tersedia di [`PRD.md`](PRD.md). Prinsip intinya:

- Local-first; data tetap di mesin pengguna.
- SQLite sebagai penyimpanan tunggal.
- Binary CLI tunggal.
- Tanpa cloud, embedding, vector database, atau MCP.
