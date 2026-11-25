import { execFile } from 'child_process';

/** Run `sdkit ext <args>` and parse its one JSON document; the binary's own message is the error. */
export function sdkitJson<T>(binary: string, cwd: string, args: string[], extraArgs: string[] = []): Promise<T> {
  return new Promise((resolve, reject) => {
    execFile(binary, ['ext', ...args, ...extraArgs], { cwd, maxBuffer: 16 << 20 }, (err, stdout, stderr) => {
      if (err) return reject(new Error((stderr || String(err)).trim()));
      try { resolve(JSON.parse(stdout) as T); } catch { reject(new Error('sdkit printed something that is not JSON')); }
    });
  });
}

/** Like sdkitJson, but with a JSON document on the standard input: a token must never be on a command line, where any program can read it. */
export function sdkitJsonWithInput<T>(binary: string, cwd: string, args: string[], input: unknown): Promise<T> {
  return new Promise((resolve, reject) => {
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
    const child = execFile(binary, ['ext', ...args], { cwd, maxBuffer: 16 << 20 }, (err, stdout, stderr) => {
      if (err) return reject(new Error((stderr || String(err)).trim().replace(/^sdkit ext:\s*/, '')));
      try { resolve(JSON.parse(stdout) as T); } catch { reject(new Error('sdkit printed something that is not JSON')); }
    });
    child.stdin!.end(text);
  });
}
