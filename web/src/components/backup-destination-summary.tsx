import { Fragment } from "react";
import { Badge } from "@/components/ui/badge";
import type { Destination } from "@/lib/backup-policies";

export function BackupDestinationSummary({
  destinations,
}: {
  destinations?: Destination[];
}) {
  if (!destinations?.length)
    return <p className="small-note">尚未配置存储位置</p>;

  return (
    <div className="policy-destination-summary" aria-label="存储位置">
      {destinations.map((destination, index) => {
        const parts =
          destination.type === "local"
            ? ["本地", destination.path]
            : destination.type === "gdrive"
              ? ["gdrive", destination.account, destination.folder_id]
              : ["rclone", destination.remote, destination.path || "根目录"];
        return (
          <Badge
            asChild
            variant="outline"
            className="policy-destination-badge"
            key={index}
          >
            <ol aria-label={`存储位置 ${index + 1}`}>
              {parts.map((part, partIndex) => (
                <Fragment key={partIndex}>
                  {partIndex > 0 && (
                    <li
                      className="policy-destination-separator"
                      aria-hidden="true"
                    >
                      /
                    </li>
                  )}
                  <li className="policy-destination-part">{part}</li>
                </Fragment>
              ))}
            </ol>
          </Badge>
        );
      })}
    </div>
  );
}
