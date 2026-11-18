const providers = [
  { match: /zerossl/i, label: "ZeroSSL", hue: 38 },
  { match: /let'?s encrypt/i, label: "Let's Encrypt", hue: 210 },
  { match: /google trust/i, label: "Google Trust Services", hue: 145 },
  { match: /digicert/i, label: "DigiCert", hue: 270 },
  { match: /sectigo/i, label: "Sectigo", hue: 340 },
  { match: /globalsign/i, label: "GlobalSign", hue: 185 },
  { match: /cloudflare/i, label: "Cloudflare", hue: 20 },
];

export function certificateIssuer(issuer: string) {
  const provider = providers.find(({ match }) => match.test(issuer));
  const label =
    provider?.label ||
    issuer.match(/(?:^|,)\s*O=([^,]+)/)?.[1] ||
    issuer.match(/(?:^|,)\s*CN=([^,]+)/)?.[1] ||
    issuer ||
    "未知签发者";
  const hue =
    provider?.hue ??
    Array.from(label.toLowerCase()).reduce(
      (hash, character) => (hash * 31 + character.charCodeAt(0)) % 360,
      0,
    );
  return { label, hue };
}
