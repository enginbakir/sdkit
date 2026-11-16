# Proves the Windows sdkit binary starts and enforces. Needs sdkit.exe and git on PATH. Windows PowerShell 5.1 or pwsh.
$ErrorActionPreference = 'Continue'
$fail = 0
function Check($name, $ok) {
  if ($ok) { Write-Host "ok    $name" } else { Write-Host "FAIL  $name"; $script:fail = 1 }
}
# stdin JSON goes through a BOM-less UTF-8 file: Windows PowerShell 5.1 would pipe a BOM or ASCII-mangled bytes.
function Hook($json) {
  $f = Join-Path $repo 'stdin.json'
  [System.IO.File]::WriteAllText($f, $json, [System.Text.UTF8Encoding]::new($false))
  $out = (cmd /c "sdkit hook --host claude PreToolUse < `"$f`"") -join ''
  return @{ Out = $out; Code = $LASTEXITCODE }
}

$repo = Join-Path ([System.IO.Path]::GetTempPath()) ("sdkit-smoke-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $repo | Out-Null
Set-Location $repo
git init -q
git config user.email t@t
git config user.name t

$v = (sdkit version) -join ''
Check 'version' ($LASTEXITCODE -eq 0 -and $v -match '^sdkit \d')

sdkit init --host claude,codex --stack go | Out-Null
Check 'init' ($LASTEXITCODE -eq 0 -and (Test-Path .sdkit/sdkit.env) -and (Test-Path AGENTS.md))

$r = Hook '{"tool_name":"Read","tool_input":{"file_path":".env"},"hook_event_name":"PreToolUse"}'
Check 'hook denies a secret path' ($r.Code -eq 0 -and $r.Out -like '*"permissionDecision":"deny"*')

$r = Hook '{"tool_name": '
Check 'malformed JSON: exit 0, no output' ($r.Code -eq 0 -and $r.Out -eq '')

sdkit sdd gate edit src/a.go | Out-Null
Check 'edit before RED is refused' ($LASTEXITCODE -eq 1)

Set-Location $env:TEMP
Remove-Item -Recurse -Force $repo -ErrorAction SilentlyContinue
exit $fail
