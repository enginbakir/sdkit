/** The Connect form, as data: what to ask, in order, and how an empty answer is read. The editor's input boxes are in extension.ts. */
export interface Connection {
  jiraBaseUrl: string; jiraProject: string; jiraUser: string; jiraTokenSet: boolean;
  bitbucketEmail: string; bitbucketTokenSet: boolean;
}
export interface Field { key: keyof ConnectionInput; prompt: string; placeholder?: string; value?: string; secret?: boolean; optional?: boolean }
export interface ConnectionInput {
  jiraBaseUrl: string; jiraProject: string; jiraUser: string; jiraToken: string; bitbucketEmail: string; bitbucketToken: string;
}

export function fields(c: Connection): Field[] {
  return [
    { key: 'jiraBaseUrl', prompt: 'Jira adresi', placeholder: 'https://sirketiniz.atlassian.net', value: c.jiraBaseUrl },
    { key: 'jiraProject', prompt: 'Jira proje anahtarı', placeholder: 'TFF', value: c.jiraProject },
    { key: 'jiraUser', prompt: 'Jira e-posta adresiniz', value: c.jiraUser },
    // an empty token keeps the saved one: the binary never sends a token back, so the box cannot be pre-filled
    { key: 'jiraToken', prompt: c.jiraTokenSet ? 'Jira token (boş bırakırsanız kayıtlı olan korunur)' : 'Jira API token', secret: true, optional: c.jiraTokenSet },
    { key: 'bitbucketEmail', prompt: 'Bitbucket e-posta adresi (isteğe bağlı)', value: c.bitbucketEmail || c.jiraUser, optional: true },
    { key: 'bitbucketToken', prompt: c.bitbucketTokenSet ? 'Bitbucket token (boş bırakırsanız kayıtlı olan korunur)' : 'Bitbucket token (isteğe bağlı)', secret: true, optional: true },
  ];
}

export interface Check { ok: boolean; who?: string; message?: string }
export interface ConnectResult { saved: boolean; jira: Check; bitbucket?: Check }

/** One line per service for the notification after saving. */
export function describeResult(r: ConnectResult): { text: string; ok: boolean } {
  const line = (name: string, c: Check) => (c.ok ? `${name}: bağlandı${c.who ? ` (${c.who})` : ''}` : `${name}: ${c.message ?? 'çalışmıyor'}`);
  const parts = [line('Jira', r.jira), ...(r.bitbucket ? [line('Bitbucket', r.bitbucket)] : [])];
  return { text: parts.join(' · '), ok: r.jira.ok && (r.bitbucket?.ok ?? true) };
}
