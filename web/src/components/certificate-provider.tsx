export function CertificateProvider({ name }: { name: string }) {
  const provider =
    name === "ZeroSSL"
      ? { id: "zerossl", icon: "/brands/zerossl.png" }
      : name === "Let's Encrypt"
        ? { id: "letsencrypt", icon: "/brands/letsencrypt.svg" }
        : null;
  return (
    <span className="certificate-provider">
      {provider && (
        <img
          src={provider.icon}
          data-provider={provider.id}
          width={16}
          height={16}
          alt=""
          aria-hidden="true"
        />
      )}
      <span>{name}</span>
    </span>
  );
}
