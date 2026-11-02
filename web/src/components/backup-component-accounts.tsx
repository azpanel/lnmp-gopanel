export type BackupAccounts = {
  status: "missing" | "ready" | "failed" | "encrypted";
  items: { name: string; type: string; current?: boolean }[];
  error?: string;
};

export function BackupComponentAccounts({
  name,
  accounts,
}: {
  name: "gdrive" | "rclone";
  accounts?: BackupAccounts;
}) {
  const configPath =
    name === "gdrive"
      ? "/root/.config/gdrive3"
      : "/root/.config/rclone/rclone.conf";
  return (
    <section className="backup-accounts" aria-label={`${name} 账户信息`}>
      <h3>
        账户信息{accounts?.items.length ? ` · ${accounts.items.length} 个` : ""}
      </h3>
      {!accounts ? (
        <p className="small-note">账户信息暂不可用，请刷新重试。</p>
      ) : accounts.status === "failed" ? (
        <p className="small-note" role="alert">
          {accounts.error || "账户信息读取失败，请检查配置后刷新。"}
        </p>
      ) : accounts.status === "encrypted" ? (
        <p className="small-note">配置文件已加密，无法读取账户信息。</p>
      ) : accounts.items.length ? (
        <ul className="backup-account-list">
          {accounts.items.map((account) => (
            <li key={account.name}>
              <span className="backup-account-name">{account.name}</span>
              <span className="backup-account-type">
                {account.type === "drive" ? "Google Drive" : account.type}
              </span>
              {account.current && (
                <span className="backup-account-current">当前账户</span>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="small-note">尚未配置账户。</p>
      )}
      <p className="small-note backup-account-path">配置位置：{configPath}</p>
    </section>
  );
}
