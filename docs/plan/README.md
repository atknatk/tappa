# Tappa — çalışma planı

Bu klasör Tappa'nın **iş defteridir**. Tek amacı var: projeyi hiç görmemiş bir
geliştirici — ya da sıfır hafızalı bir ajan — buraya bakıp *ne yapıldığını, ne
yapılacağını ve sıradaki işin tam olarak ne olduğunu* tereddütsüz anlayabilsin.
Hiçbir adımı atlamadan, önceki işi bozmadan.

## Dosyalar ve rolleri

| Dosya | Rol | Ne zaman değişir |
|---|---|---|
| **[state.md](state.md)** | **Tek canlı durum kaynağı.** Nerede kaldık, sırada ne var, hangi görev bitti/bloke, hangi commit'te | **Her oturumun sonunda** |
| [roadmap.md](roadmap.md) | Kilometre taşları, sıralama gerekçesi, riskler, kapsam dışı | Ürün veya sıra kararı değişince |
| `m0…m9-*.md` | **Görev kartları** — her görevin değişmez spec'i | Kapsam değişirse (gerekçesi yazılarak) |
| [agent-brief.md](agent-brief.md) | Yapıcı/denetçi brief şablonları, sabit kurallar, öğrenilen dersler | Yeni bir ders çıkınca |
| [open-questions.md](open-questions.md) | Karara bağlanmamış konular, hangi görevi blokladıkları | Soru çıkınca / cevaplanınca |

> **Durum yalnızca `state.md`'de tutulur.** Görev kartlarına "yapıldı" işareti
> konmaz. İki yerde tutulan durum er geç çelişir, sıfır hafızalı ajan da hangisine
> güveneceğini bilemez. Kart *ne demek istediğimizi* söyler, `state.md` *nerede
> olduğumuzu*.

## Oturum protokolü — zorunlu

Bu 6 adım her oturumda, istisnasız uygulanır.

**1. Kuralları yükle.** Repo kökündeki [CLAUDE.md](../../CLAUDE.md) — özellikle §4
(kırmızı çizgiler) ve ilgili bölüm. Ürün bağlamı gerekiyorsa
[docs/handoff.md](../handoff.md).

**2. Nerede olduğunu öğren.** [state.md](state.md) → "ŞU AN" bölümü sıradaki
görev ID'sini verir (`M1-03` gibi).

**3. Ortamı doğrula.** `state.md` içindeki *sağlık kontrolü* komutlarını çalıştır.
Beklenen çıktıyı vermiyorsa **önce onu düzelt** — çürük zeminde iş yapma.

**4. Görev kartını aç.** `docs/plan/m<N>-*.md` dosyasında görev ID'sinin başlığına
git. Kart şunları verir: amaç, neden, ön koşullar, dokunulacak dosyalar, adımlar,
kabul kriterleri, tuzaklar.

**5. İşi yap.** Kartta *kırmızı çizgi* işareti varsa ilgili CLAUDE.md §4 maddesini
tekrar oku. Şüpheye düşersen **dur ve sor** — sessizce yorumlama.

**5b. Üçüncü göz — ZORUNLU.** İş biter bitmez, işi **yapmayan** ayrı bir denetçi
ajan çalıştırılır. Denetçi kartın kabul kriterlerini tek tek doğrular ve bulgu
raporlar. **Bulgu varsa düzeltilir ve yeniden denetlenir**; onay gelmeden görev
`done` işaretlenmez ve bir sonraki göreve geçilmez. Ayrıntı: aşağıdaki
"Çalışma modu".

**6. Kapat.** Şu sırayla:
   1. `make check` yeşil (fmt + lint + test, üretilen dosyalar commit edilmiş).
   2. Kırmızı çizgiye değen bir iş yaptıysan `make audit` + agent
      `tappa-security-auditor`.
   3. Commit (İngilizce, emir kipi, `type(scope): summary`).
   4. **`state.md` güncelle**: ledger satırının durumu + commit hash'i, "ŞU AN"
      pointer'ı bir sonraki göreve, "son oturum" notu.
   5. Yeni belirsizlik çıktıysa [open-questions.md](open-questions.md)'ye ekle.

Adım 6.4 atlanırsa bir sonraki oturum yanlış yerden başlar. Bu, bu klasörün tek
gerçek başarısızlık modudur.

## Çalışma modu — orkestrasyon + üçüncü göz

*(2026-07-24'ten itibaren geçerli.)*

Ana oturum **iş yapmaz, organize eder**. Her görev için:

```
1. YAPICI       alt ajan (model: opus) → görev kartını uygular
2. ÜÇÜNCÜ GÖZ   ayrı alt ajan (model: opus) → kabul kriterlerini denetler
3. Bulgu var mı?
   ├─ EVET → yapıcıya düzelttir → 2'ye dön (yeni bir denetçiyle)
   └─ HAYIR → görev `done`, state.md güncellenir → sonraki göreve geç
```

**Kurallar:**
- Denetçi, işi yapan ajanın **kendisi olamaz**. Kendi işini denetleyen ajan
  kendi varsayımlarını doğrular.
- Denetçi **kod değiştirmez**, yalnız bulgu üretir; her bulgu `dosya:satır` +
  somut sonuç ile.
- Denetim raporu kullanıcıya **olduğu gibi** aktarılır. "Temiz geçti" diye
  özetleyip bulgu yutulmaz.
- Kırmızı çizgiye değen görevlerde denetçiye ek olarak agent
  `tappa-security-auditor` da koşar.
- Onay gelmeden `state.md`'de `done` yazılmaz.

**Brief'lerin şekli ve her turda tekrarlanan kurallar:**
[agent-brief.md](agent-brief.md). Yapıcı ve denetçi şablonları, sabit kurallar
ve şimdiye kadar öğrenilen dersler orada — bağlam sıkışsa bile disiplin kaybolmasın.

**Neden:** 2026-07-24'te plan üç bağımsız ajana denetletildi ve ana oturumun
gözden kaçırdığı ciddi hatalar çıktı — en ağırı, bir güvenlik açığının
(A1, URL biriktirme) yanlışlıkla "çözüldü" işaretlenmesiydi. Tek gözün
yetmediği somut olarak görüldü. M0 yürütmesinde de dört görevin dördü ilk
turda RED aldı ve her seferinde gerçek bir kusur çıktı.

## Görev ID şeması

`M<kilometre taşı>-<sıra>` → `M2-05`. ID'ler **yeniden kullanılmaz**. Bir görev
iptal edilirse ledger'da `skipped` olarak kalır ve nedeni yazılır; numara boşta
bırakılmaz.

Yeni görev ihtiyacı çıkarsa ilgili milestone dosyasının sonuna eklenir (sıradaki
boş numara ile), ledger'a satır açılır, `state.md`'de neden eklendiği not düşülür.

## Görev kartı formatı

````markdown
## M1-03 — locations & departments migration

- **Bağımlılık:** M1-02
- **Kırmızı çizgi:** §4.5 (tenant izolasyonu) · §6 (DB kuralları)
- **Araç:** agent `tappa-db-migrator`
- **Commit:** `feat(db): add locations and departments`

**Amaç.** Bir cümlede ne teslim edilecek.

**Neden.** Bu iş neden şimdi, neye hizmet ediyor.

**Ön koşullar.** İşe başlamadan doğru olması gerekenler.

**Dokunulacak dosyalar.** Beklenen dosya listesi (kesin değil, yön verir).

**Adımlar.** Sıralı, uygulanabilir.

**Kabul kriterleri.** Ölçülebilir, doğrulanabilir maddeler. Hepsi sağlanmadan
görev `done` işaretlenmez.

**Tuzaklar.** Bu işte daha önce kafa karıştırmış veya kolayca yanlış yapılan şeyler.
````

## Sözlük

Sıfır hafızalı ajanın kod ve dokümanda karşılaşacağı proje terimleri.

| Terim | Anlamı |
|---|---|
| **plaket / wall tag** | Lokasyon girişine monte edilen pasif NFC plaketi (NTAG 424 DNA). Çalışanda değil **duvarda**. |
| **tap** | Çalışanın telefonunu plakete değdirmesi → tarayıcının açılması → tek işlem. |
| **SUN** | Secure Unique NFC. Çipin her okutmada ürettiği `ctr` + CMAC imzası. *Proof of moment*. |
| **SDM** | Secure Dynamic Messaging. NTAG 424'ün SUN üretme özelliği. |
| **ctr** | Etiketin okuma sayacı. 3 byte, monoton artar. Replay korumasının çekirdeği. |
| **CMAC** | AES tabanlı imza. Çip 16 byte'ın tek indeksli 8 byte'ını yayınlar. |
| **KEK** | Key Encryption Key. Etiket AES anahtarlarını sarmalayan ana anahtar (`TAPPA_TAG_KEK`). |
| **verdict** | Bir tap'in sonucu: `ok` · `flag` · `reject` · `ignored`. |
| **flag / FLAGGED** | Kanıt yetersiz ama kayıt alındı; müdür onay kuyruğuna düştü. Asla sessizce atılmaz. |
| **trust** | Güven puanı: `20 taban + 50 (IP eşleşti) + 30 (GPS eşleşti)`. |
| **debounce** | Aynı **kişinin** 60 sn içindeki tekrar tap'i → `ignored`. Etiket bazlı **değil**. |
| **practice tap** | Aktivasyon sonrası ilk kayıt. `practice=true`, TRAINING damgası, çalışılan saate sayılmaz. |
| **docket** | Mutfak adisyonu. İşlem kayıtlarının görsel motifi — perforeli fiş kartı. |
| **kaşe / stamp** | Docket üstündeki hafif eğik APPROVED/FLAGGED/REJECTED damgası. |
| **channel** | Tap'in geldiği yol: `nfc` · `qr` · `manual`. |
| **tenant** | Müşteri firma. KF = Kebab Factory, KM = Kebab Manufacturing. |
| **RLS** | Row-Level Security. Postgres'in satır düzeyi tenant izolasyonu. |
| **policy** | Bir kararı belirleyen kural belgesi. AWS IAM biçimi: `statement` · `effect` · `action` · `resource` · `condition`. Bkz. [M3](m3-policy-motoru.md). |
| **effect** | Bir politikanın sonucu: `allow` → `ok` · `review` → `flag` · `deny` → `reject` · `ignore` → `ignored`. |
| **guardrail** | Sistem katmanı politikası. Kodda gömülü, **kapatılamaz**, eşleşirse terminal. §4 kırmızı çizgileri ve §5 satır 1–5. |
| **baseline** | Tappa'nın yönetilen politikası; her tenant'a varsayılan bağlı, tenant değiştirebilir. §5 satır 6–7. |
| **sınırlı parametre** | Kapatılamayan ama aralık içinde ayarlanabilen koruma (ör. tazelik penceresi 1–15 dk). |
| **fail-to-review** | Hiçbir politika eşleşmezse sonuç `review`'dur, `deny` değil — §4.6 gereği kayıt asla kaybolmaz. IAM'in "implicit deny"inden bilinçli sapma. |
| **tappa_app / tappa_owner** | Uygulama rolü (RLS'e tabi) / migration rolü (şema sahibi). Karıştırılırsa izolasyon çöker. |
