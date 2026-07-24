---
name: tappa-next
description: Tappa'da "kaldığımız yerden devam et" protokolü. Oturuma başlarken, "sırada ne var", "devam edelim", "nerede kalmıştık" dendiğinde veya bir görevi bitirip durumu kapatırken oku. docs/plan/state.md'yi tek doğru kaynak kabul eder.
---

# Kaldığımız yerden devam

Bu skill [docs/plan/README.md](../../../docs/plan/README.md)'deki oturum
protokolünü uygular. Amaç: hafızası olmayan bir ajanın bile doğru yerden,
hiçbir adımı atlamadan devam etmesi.

## Başlarken — sırayla

**1. Durumu oku.** [docs/plan/state.md](../../../docs/plan/state.md) → "ŞU AN"
bölümü sıradaki görev ID'sini ve blokeleri verir. **Bu dosya tek doğru
kaynaktır** — görev kartlarındaki metinden ya da sohbet geçmişinden durum
çıkarma.

**2. Sağlığı doğrula.** `state.md`'deki *Sağlık kontrolü* tablosundaki komutları
çalıştır. Uymayan satır varsa önce onu düzelt; çürük zeminde iş yapma.

**3. Kartı aç.** `docs/plan/m<N>-*.md` içinde görev ID'sinin başlığına git.
Kart: amaç · neden · bağımlılık · dokunulacak dosyalar · adımlar · kabul
kriterleri · tuzaklar.

**4. Bağımlılıkları ve açık soruları kontrol et.**
- Kartın **Bağımlılık** satırındaki görevler `done` mu? Değilse önce onlar.
- Kart bir `Q` numarası gösteriyorsa
  [open-questions.md](../../../docs/plan/open-questions.md)'ye bak. Soru hâlâ
  açıksa ve kararı değiştiriyorsa **kullanıcıya sor**, varsayımla ilerleme.

**5. Aracı kullan.** Kartta yazan skill/agent'ı gerçekten çağır:

| Kart ne diyorsa | Çağır |
|---|---|
| migration / tablo / sqlc sorgusu | agent `tappa-db-migrator` |
| templ / Tailwind / herhangi bir ekran | skill `tappa-brand` |
| SUN / CMAC / ctr / NFC | skill `tappa-sun` |
| seed / fixture / demo verisi | skill `tappa-seed` |
| kırmızı çizgiye değen değişiklik | agent `tappa-security-auditor` |

**6. Kırmızı çizgi kontrolü.** Kartta `Kırmızı çizgi` satırı varsa
[CLAUDE.md](../../../CLAUDE.md) §4'ün ilgili maddesini oku. Şüphe varsa **dur ve
sor** — sessizce yorumlama.

## Çalışma modu — orkestrasyon + üçüncü göz

Ana oturum **iş yapmaz**. Görevi bir alt ajana (model `opus`) yaptır, sonra
**ayrı** bir üçüncü göz ajanına (yine `opus`) denetlet. Brief şablonları, her
turda tekrarlanan sabit kurallar ve şimdiye kadar öğrenilen dersler:
[docs/plan/agent-brief.md](../../../docs/plan/agent-brief.md) — brief yazmadan
önce oku. Bulgu varsa düzelttir ve
**yeni bir denetçiyle** tekrar denetlet. Onay gelmeden `done` yazma, sonraki
göreve geçme. Denetçi işi yapan ajan olamaz; kod değiştirmez, yalnız bulgu
üretir. Rapor kullanıcıya olduğu gibi aktarılır.

## Bitirirken — sırayla, atlamadan

0. Üçüncü göz **onay verdi** mi? Vermediyse görev bitmemiştir.
1. `make check` yeşil.
2. Kırmızı çizgiye değdiyse `make audit` + agent `tappa-security-auditor`.
3. Kabul kriterlerinin **hepsi** sağlandı mı — tek tek geç. Sağlanmayan varsa
   görev `done` değildir; `wip` bırak ve nedenini yaz.
4. Commit: İngilizce, emir kipi, `type(scope): summary`. Kart genelde beklenen
   mesajı veriyor.
5. **`state.md`'yi güncelle** — bu adım atlanırsa bir sonraki oturum yanlış
   yerden başlar:
   - Ledger satırı: durum + commit hash
   - "ŞU AN" bloğu: sıradaki görev, dal, blokeler
   - "Özet" sayaçları
   - "Oturum günlüğü"ne en üste kısa not (ne yapıldı, ne öğrenildi, ne kaldı)
   - "Son güncelleme" tarihi
6. Yeni belirsizlik çıktıysa `open-questions.md`'ye satır ekle (bloklar,
   sahip, durum). Bir soru cevaplandıysa "Cevaplananlar"a taşı.
7. Kapsam değiştiyse görev kartını güncelle ve **neden** değiştiğini yaz.

## Kurallar

- **Durum yalnızca `state.md`'de.** Karta "yapıldı" yazma, ikinci bir liste tutma.
- **Görev ID'leri yeniden kullanılmaz.** İptal edilen görev `skipped` olarak kalır.
- **Yeni görev** ilgili milestone dosyasının sonuna eklenir, ledger'a satır açılır.
- **Sıra atlanabilir** ama gerekçesi `state.md` oturum günlüğüne yazılır —
  sıfır hafızalı ajan neden atlandığını görebilmeli.
- `main`'e doğrudan commit yok (ilk commit hariç); iş dalda yapılır. Kullanıcı
  istemedikçe push/PR açma.
