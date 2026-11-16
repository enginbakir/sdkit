# LOGIN-V2 oturumu: kök neden analizi ve uygulama planı

Kaynak: koddan okunanlar (`internal/review`, `internal/tasks`, `internal/spec`, `internal/jira`, `internal/pr`,
`templates/review.d/*.env`) ve skill metinleri (`generate-specs`, `generate-tasks`, `review.md`, `triage.md`,
`propose.md`, `task-format.md`). Oturumun kendisi (LOGIN-V2 girdisi) repoda YOK, bkz. "Açık noktalar".

Değişmezler hiçbir önerinin gerekçesiyle gevşemiyor: PASS onay değil, taze inceleyici, `isolation=none` asla PASS,
`accept` yalnız açık evet sonrası, `publish` taze PASS ister, ajan main'e dokunmaz.

## Sorun 1: EGM maliyeti

### Kök nedenler (kodda doğrulanan)
1. **`Begin` her çağrıda tur yakar.** `review.go` `Begin`: `st.Round = prev + 1`, hash'e bakılmıyor. Önceki turun kanıtı aynı
   içerik hash'ine bağlıysa yeni tur hiçbir şey öğrenmez. (Oturumdaki "boşa giden tur" bu.)
2. **Review kaba taneli.** Hash tüm dizin için tek (`Hash(t)`); bir dosyadaki tek harf, üç artifact'ın hepsini ve her
   inceleyiciyi yeniden çalıştırtıyor. Dosya/bölüm bazında kanıt tutulmuyor.
3. **Rubric turdan bağımsız.** `review.d/<artifact>.env` sabit `REVIEWERS`; 3. turda da 1. turdaki 4 inceleyici çalışır.
4. **Mekanik tutarlılık için LLM kullanılıyor.** Kitte AC-id/atıf/liste eşitliği kontrolü yok; pahalı inceleyici bunları
   tur tur yeniden buluyor ve her tur "ilk ~10 bulguyu" örneklediği için yakınsama gürültülü.
5. **Kalıcı bağlam yok.** "Settled" listesi orkestratörün prompt'unda yaşıyor; analist cevapları da kayıtlı değil.
6. **Kapsam büyümesi frenlenmiyor.** İnceleme bulgusu → "ekle" düzeltmesi; yeni AC'nin kaynağı kayıtlı değil. 32 → 124 AC.
7. **Yakınsama kriteri yok**: cap'e dayanana kadar tur açılıyor; cap'i yükseltmek `sdkit.env` elle düzenleme.

### Öneriler: değerlendirme
| # | Karar | Gerekçe / değişiklik |
|---|---|---|
| 1a lint | **Kabul, genel tut** | Kit yalnız `AC-<feature>-<n>` + `(withdrawn)` bilir; R-/D-/V-/Q-/C- projeye özgü, üretici uyduruyor. Lint: AC benzersizlik/sıra/withdrawn, artifact'lar arası AC listesi eşitliği, tanımlı-olmayan atıf. Önek listesi `sdkit.env`'den (`LINT_REF_PREFIXES`), kodda sabit değil. "Lint temizlenmeden LLM turu açılamaz" = `begin`'de bayrakla (`REVIEW_REQUIRE_LINT=1`), varsayılan kapalı (geriye uyum). |
| 1b kademeli inceleme | **Kabul, bir koşulla** | Round-1 ucuz PASS'i `publish`'e yetmemeli. Önerim: kanıt, hangi küme (`cheap`/`full`) ile alındığını taşır; `check` yalnız son içerikte `full` kümeyle alınmış PASS'i kabul eder. Bu bir tasarım kararı, bkz. sorular. |
| 1c değişen artifact | **Kısmen** | Dosya bazı: güvenli, kabul. H2 bölümü bazı: çapraz-atıf yüzünden "no finding" taşımak riskli (R-3 değişirse başka bölümdeki AC anlamı değişir). Yalnız bölümün hash'i VE atıf verdiği id kümesi değişmediyse taşı; ilk PR'da yalnız dosya bazı. |
| 1d settled | **Kabul, korkulukla** | `settle` yalnız kullanıcıdan gelir (`--who`/`JIRA_USER`, `accept` ile aynı kural); aksi hâlde orkestratör bulguyu "settled" diye susturup `accept` değişmezini dolanır. Settled = inceleyici prompt'una girdi + `record` yinelenen konuda uyarır; bulguyu OTOMATİK silmez. |
| 1e scope-growth | **Kabul, hafif** | `begin`, önceki tura göre eklenen/çıkarılan AC id sayısını hesaplar; eşik (`SPEC_REVIEW_MAX_NEW_ACS`) üstünde uyarı. "Her AC'nin kaynağı metadata'da" spec biçimini değiştirir, ertelemeyi öneriyorum (soru). |
| 1f yakınsama + idempotent begin | **Kabul** | Idempotent begin en yüksek getiri/maliyet. Yakınsama sunumu `review status` çıktısı + skill metni; otomatik PASS asla üretmez. |
| 1g görünürlük | **Kabul** | `review status <id>`: tur, bulgu sayıları trendi, rol çalıştırma sayısı. Token/$ için `run-role` kullanım kaydının review id'ye bağlanabildiği doğrulanmalı (`internal/usage`), yoksa yalnız rol sayısı. |

## Sorun 2: görev ayrıntısı

### Kök nedenler
1. `task-format.md` yalnız "atomic: bir inceleyicinin kafasında tutabileceği" diyor: sayısal tavan/oran/ayırma kriteri yok.
2. `tasks acs`/`allACs` metindeki her `AC-…` belirtecini toplar (`acToken`); `(withdrawn)` işaretini hiç görmez. `trace`
   da bu listeyle çalışır, geri çekilmiş AC "görevsiz" çıkar.
3. Spec'teki "bekleyen/koşullu Qn" işareti makine-okunur bir biçimde değil, serbest metin.
4. Risk rubric'i `project.md`'de TODO iken hiçbir kapı yok; ajan "medium" öneriyor.
5. Yeniden gruplama aracı yok; Jira'daki eski görevleri kapatmak elle.

### Öneriler: değerlendirme
| # | Karar | Gerekçe |
|---|---|---|
| 2a boyut kuralı | **Kabul, formülü reddet** | √(AC)×2, LOGIN-V2 için 22 verir (124 AC): oturumun kendi hedefi 15-17 ve önerilen varsayılan 12-20 ile çelişir. Yerine: `TASKS_TARGET_MIN/MAX` (12/20) ve görev başına 4-12 AC oranı; ikisinin kesişimi (124 AC için 12-20). |
| 2b plan-check | **Kabul** | Salt-okunur, deterministik; uyarı üretir, kapı değil (bayrakla hata). Birleştirme adayı: aynı kapı + aynı kapsam öneki + zincir bağımlılık. |
| 2c Q-kapılı AC | **Kabul, biçime bağlı** | Önce spec'te işaretin makine-okunur biçimi gerekli (bir satır: `blocked-by: Q3`). Bu bir şablon kararı, bkz. sorular. |
| 2d withdrawn | **Kabul, tartışmasız** | `acs`/`trace` varsayılan atlar, `--include-withdrawn` opsiyonel. Biçim zaten kanonik: `#### Scenario: AC-x-n (withdrawn) - …`. |
| 2e risk TODO | **Kısmen** | `plan-check` rubric TODO ise açık hata verir. Doldurma sihirbazı: reddet (kapsam şişmesi); yerine hata mesajı doldurulacak soruları listeler. |
| 2f regroup/supersede | **Kabul, geç sıra** | `tasks regroup` yerel ve güvenli. `jira supersede` dış sisteme yazar: geçiş adı projeye özgü, silme yok, onay ister; ayrı PR, tasarım sorusu var. |

## Sorun 3: analist yorgunluğu

### Kök nedenler
1. Sorular tur içinde, bulgudan bulguya çıkıyor (triage.md "ASK" sonra uygula sonra yeni tur) → ~25 soru parça parça.
2. Analist cevabı kalıcı değil: her turda yeniden tartışılabilir (= 1d ile aynı eksik).
3. `spec publish`: `envAt.get`, `BITBUCKET_TOKEN/EMAIL` için yalnız süreç ortamına bakıyor (`k == "BITBUCKET_TOKEN" ... return v`),
   `sdkit.local.env` okunmuyor; oysa docs (`guide.md` 690) bunu vaat ediyor. **Hata, düzeltilecek.** Push hatasında git stderr
   atılıyor (`gok` yalnız bool döner).
4. Branş adı `spec/<id>` büyük/küçük harfe duyarsız FS'te çakışabiliyor; ön-kontrol yok.
5. Hata mesajı/girdi doğrulama: SHA uzunluğu, parent varlığı, `jira upsert` JSON'u argüman metni bekliyor.
6. `promote` ayrı adım; `publish` çıktısı sonraki adımları söylemiyor.

### Öneriler: değerlendirme
| # | Karar | Gerekçe |
|---|---|---|
| 3a `spec make` | **Reddet (ikili içinde), alternatif öner** | Kit'in ikilisi deterministik; LLM'i yürütmüyor, skill yürütüyor (CLAUDE.md: roller `run-role` ile dışarıda). `make` ikiliye ajan döngüsü sokar. Alternatif: `sdkit spec next <id>` bir sonraki adımı ve "insan gerekli mi" bilgisini yazdırır; skill "run to PASS" modunu bunun üstünde yürütür. Tasarım sorusu. |
| 3b soru toplama | **Kabul** | Skill adımı: pre-spec'ten tüm açık noktaları başta topla, karar/gerçek/varsayılan sınıfla, toplu varsayılan-kabul + decisions.md. |
| 3c decisions.md | **Kabul, 1d ile birleştir** | Tek depo. Kapsam (spec başına mı, proje geneli mi) ve commit edilip edilmeyeceği sorusu var. |
| 3d raise-cap | **Kabul** | Cap `state.json`'a (id başına) yazılır, audit satırı, `accept` ile aynı kimlik kuralı; kullanıcının açık evetini kod doğrulayamaz, skill kuralı kalır (accept'te de böyle). |
| 3e preflight | **Kabul** | Her kontrol ayrı, saf fonksiyon; ağ gerektirenler `--offline` ile atlanır. |
| 3f publish | **Kabul** | local.env düzeltmesi + git stderr (token maskeli) + bitiş mesajında sonraki adımlar. Promote'u publish'e gömmek: ayrı karar, `approved` durumu Tech Lead merge'ünden önce yazılırdı, mevcut modelin tersi; önerim gömmemek, sadece sonraki adımı söylemek. |
| 3g branş adı | **Ön-kontrol, normalize etme** | Normalize, mevcut `spec/<id>` dallarını (approved-sha, refChanges, drift hepsi `"spec/"+id` kullanır) kırar. |
| 3h hijyen | **Kabul** | `jira upsert` dosya yolu kabul eder; hata mesajı beklenen biçimi söyler. `tasks ids` kodu zaten `externalId` yazıyor (`IDs()`); tüm belgeyi basması adıyla uyuşmuyor, çıktıyı doğrulayıp ona göre ya adı ya çıktıyı düzeltirim. |
| 3i şablon sadeleştirme | **Kabul, soru** | Üretici talimatı (`generate.md`) hangi kodları vereceğini söylemiyor; sadeleştirme prompt/şablon kararı. |

## Maliyet/kazanç sırası (uygulama sırası)
| Sıra | İş | Etki | Maliyet | Risk |
|---|---|---|---|---|
| 1 | 1f idempotent `begin` | Boşa tur biter | S | düşük |
| 2 | 2d withdrawn | görev planı doğru | S | düşük |
| 3 | 3f publish token + stderr + sonraki adım | sürtünme (a)(c)(d) | S | düşük |
| 4 | 1a lint | tur sayısını en çok düşüren | M | orta (genel kalmalı) |
| 5 | 1d/3c settled+decisions | tekrar tartışma biter | M | orta (accept değişmezi) |
| 6 | 3e preflight, 3g çakışma ön-kontrolü | sürtünme (b)(e) | M | düşük |
| 7 | 2a/2b plan-check + boyut kuralı | 39 → 12-20 | M | düşük |
| 8 | 1c dosya-bazlı kanıt, 1b kademeli, 1g status | token %60 hedefi | L | yüksek (PASS anlamı) |
| 9 | 3d raise-cap, 3h, 2e | | S | düşük |
| 10 | 2c, 2f, 3a/3b/3i | | L | tasarım kararı gerek |

## Açık noktalar (varsayım yapmıyorum)
- **LOGIN-V2 fixture repoda yok.** Kabul ölçütleri (≤4 LLM turu, ≥%50 az rol çalıştırması, 12-20 görev) gerçek pre-spec,
  son spec ve review geçmişi olmadan ölçülemez. Birim testlerle ölçebildiğim: lint'in yakaladığı hata sınıfı, `begin`'in
  tur tüketmemesi, plan-check'in 39 görevli bir `tasks.json`'da uyarısı. LLM turu sayısı deterministik koddan ölçülmez; onu
  fixture + bir oturum kaydıyla birlikte raporlarım.
- Tasarım soruları kullanıcıya soruldu (1b küme kuralı, settled kapsamı, 3a, 2c biçimi, 3i, 2f geçiş adı).
