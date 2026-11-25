import { execFile } from 'child_process';
import * as fs from 'fs';

/**
 * Why the binary could not even be started, said exactly. Node reports a missing binary and a missing working folder with the same
 * ENOENT, which sent a person looking for a file that was there.
 */
export function launchProblem(binary: string, cwd: string): string | undefined {
  if (!fs.existsSync(cwd)) return `LAUNCH:cwd:${cwd}`;
  if (/[\\/]/.test(binary)) { // a path; a bare name is looked up on PATH
    let st: fs.Stats;
    try { st = fs.statSync(binary); } catch { return `LAUNCH:missing:${binary}`; }
    if (st.isDirectory()) return `LAUNCH:directory:${binary}`;
    try { fs.accessSync(binary, fs.constants.X_OK); } catch { if (process.platform !== 'win32') return `LAUNCH:not-executable:${binary}`; }
  }
  return undefined;
}

/** Run `sdkit ext <args>` and parse its one JSON document; the binary's own message is the error. */
export function sdkitJson<T>(binary: string, cwd: string, args: string[], extraArgs: string[] = []): Promise<T> {
  return new Promise((resolve, reject) => {
    const lp = launchProblem(binary, cwd);
    if (lp) return reject(new Error(lp));
    execFile(binary, ['ext', ...args, ...extraArgs], { cwd, maxBuffer: 16 << 20 }, (err, stdout, stderr) => {
      if (err) return reject(new Error((stderr || String(err)).trim()));
      try { resolve(JSON.parse(stdout) as T); } catch { reject(new Error('sdkit printed something that is not JSON')); }
    });
  });
}

/** Like sdkitJson, but with a JSON document on the standard input: a token must never be on a command line, where any program can read it. */
export function sdkitJsonWithInput<T>(binary: string, cwd: string, args: string[], input: unknown): Promise<T> {
  return new Promise((resolve, reject) => {
    const lp = launchProblem(binary, cwd);
    if (lp) return reject(new Error(lp));
    const child = execFile(binary, ['ext', ...args], { cwd, maxBuffer: 16 << 20 }, (err, stdout, stderr) => {
      if (err) return reject(new Error((stderr || String(err)).trim().replace(/^sdkit ext:\s*/, '')));
      try { resolve(JSON.parse(stdout) as T); } catch { reject(new Error('sdkit printed something that is not JSON')); }
    });
    child.stdin!.end(JSON.stringify(input));
  });
}

/** Run `sdkit ext <args>` with plain text on the standard input (a pre-spec) and parse its JSON answer. */
export function sdkitJsonWithText<T>(binary: string, cwd: string, args: string[], text: string): Promise<T> {
  return new Promise((resolve, reject) => {
    const lp = launchProblem(binary, cwd);
    if (lp) return reject(new Error(lp));
    const child = execFile(binary, ['ext', ...args], { cwd, maxBuffer: 16 << 20 }, (err, stdout, stderr) => {
      if (err) return reject(new Error((stderr || String(err)).trim().replace(/^sdkit ext:\s*/, '')));
      try { resolve(JSON.parse(stdout) as T); } catch { reject(new Error('sdkit printed something that is not JSON')); }
    });
    child.stdin!.end(text);
  });
}
