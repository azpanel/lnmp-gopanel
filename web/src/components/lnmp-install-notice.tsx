import { Link } from "react-router-dom";
import { Download, Loader2 } from "lucide-react";
import { activeInstall, useInstallation } from "@/lib/lnmp-install";

export function LNMPInstallNotice() {
  const state = useInstallation();
  if (!state.data || (state.data.installed && !activeInstall(state.data.job)))
    return null;
  const active = activeInstall(state.data.job);
  return (
    <div className="install-notice">
      {active ? (
        <Loader2 size={20} className="animate-spin" />
      ) : (
        <Download size={20} />
      )}
      <div>
        <strong>
          {active ? "LNMP 安装正在进行" : "此服务器尚未安装 LNMP"}
        </strong>
        <p className="small-note">
          {active
            ? state.data.job?.phase
            : "选择数据库、PHP 与 Nginx 模块，在线安装并查看编译输出。"}
        </p>
      </div>
      <Link to="/lnmp/install" className="install-link">
        {active ? "查看进度" : "安装 LNMP"} →
      </Link>
    </div>
  );
}
