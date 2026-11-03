import { useEffect } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import {
  Boxes,
  CalendarDays,
  Inbox,
  KeyRound,
  LayoutDashboard,
  LogOut,
  Mic,
  Server,
  Settings2,
  X,
} from "lucide-react";
import { Mark } from "../../components";
import { Button } from "../ui/button";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "../ui/sidebar";

export const navigation = [
  { path: "/", label: "总览", icon: LayoutDashboard, group: "工作台" },
  { path: "/box", label: "收件箱", icon: Inbox, group: "工作台" },
  { path: "/audio", label: "音频记录", icon: Mic, group: "工作台" },
  { path: "/reviews", label: "每日回顾", icon: CalendarDays, group: "工作台" },
  { path: "/agents", label: "电脑与来源", icon: Server, group: "管理" },
  { path: "/nodes", label: "设备", icon: Boxes, group: "管理" },
  { path: "/mqtt", label: "MQTT 账号", icon: KeyRound, group: "管理" },
  { path: "/system", label: "系统信息", icon: Settings2, group: "管理" },
];

export function AppSidebar({ onLogout }: { onLogout: () => void }) {
  const { pathname } = useLocation();
  const { setOpenMobile, isMobile } = useSidebar();
  useEffect(() => {
    setOpenMobile(false);
  }, [pathname, setOpenMobile]);
  return (
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader>
        <div className="flex h-12 items-center gap-2 px-2">
          <Link
            to="/"
            className="flex min-w-0 items-center gap-3"
            aria-label="Orbit 工作台"
          >
            <Mark small />
            <span className="text-lg font-semibold group-data-[collapsible=icon]:hidden">
              Orbit
            </span>
          </Link>
          {isMobile && (
            <Button
              size="icon"
              variant="ghost"
              className="ml-auto"
              aria-label="关闭导航"
              onClick={() => setOpenMobile(false)}
            >
              <X />
            </Button>
          )}
        </div>
      </SidebarHeader>
      <SidebarContent>
        <nav id="main-navigation" aria-label="主导航">
          {["工作台", "管理"].map((group) => (
            <SidebarGroup key={group}>
              <SidebarGroupLabel>{group}</SidebarGroupLabel>
              <SidebarMenu>
                {navigation
                  .filter((item) => item.group === group)
                  .map((item) => (
                    <SidebarMenuItem key={item.path}>
                      <SidebarMenuButton
                        asChild
                        tooltip={item.label}
                        isActive={
                          pathname === item.path ||
                          (item.path !== "/" &&
                            pathname.startsWith(item.path + "/"))
                        }
                      >
                        <NavLink
                          to={item.path}
                          end={item.path === "/"}
                          aria-label={item.label}
                        >
                          <item.icon />
                          <span>{item.label}</span>
                        </NavLink>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
              </SidebarMenu>
            </SidebarGroup>
          ))}
        </nav>
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              onClick={onLogout}
              tooltip="退出登录"
              aria-label="退出登录"
            >
              <LogOut />
              <span>退出登录</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
