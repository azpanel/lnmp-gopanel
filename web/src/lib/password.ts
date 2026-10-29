export const passwordUserInputs = (username: string) => [
  username,
  "lnmp",
  "gopanel",
  "lnmp-gopanel",
];

export function generateStrongPassword(): string {
  const groups = [
    "ABCDEFGHJKLMNPQRSTUVWXYZ",
    "abcdefghijkmnopqrstuvwxyz",
    "23456789",
    "!@#$%*_+-=",
  ];
  const alphabet = groups.join("");
  const random = (limit: number) => {
    const byte = new Uint8Array(1);
    const ceiling = 256 - (256 % limit);
    do crypto.getRandomValues(byte);
    while (byte[0] >= ceiling);
    return byte[0] % limit;
  };
  const password = groups.map((group) => group[random(group.length)]);
  while (password.length < 24) password.push(alphabet[random(alphabet.length)]);
  for (let i = password.length - 1; i > 0; i--) {
    const j = random(i + 1);
    [password[i], password[j]] = [password[j], password[i]];
  }
  return password.join("");
}

export async function assessPassword(password: string, username: string) {
  const length = Array.from(password).length;
  const size = new TextEncoder().encode(password).length;
  if (length < 15 || size > 72)
    return {
      score: 0,
      valid: false,
      message: "至少 15 个字符，最多 72 个 UTF-8 字节",
    };
  if (/[\p{Cc}]/u.test(password))
    return { score: 0, valid: false, message: "密码不能包含控制字符" };
  const { default: zxcvbn } = await import("zxcvbn");
  const result = zxcvbn(password, passwordUserInputs(username));
  return {
    score: result.score,
    valid: result.score >= 3,
    message:
      result.score >= 3
        ? "密码强度已通过"
        : "密码容易被猜到，请增加不相关的词语或使用随机密码",
  };
}
