import { Text } from "@/components/Text/text";
import { Button } from "@/components/ui/button";
import { Ban, RotateCcw, Shield, ShieldOff } from "lucide-react";
import React from "react";
import { formatDate } from "./formatDate";

interface AdminAccount {
  id: string;
  name: string;
  email: string;
  installation_admin: boolean;
  deactivated: boolean;
  created_at?: string;
}

interface AccountRowProps {
  acc: AdminAccount;
  isSelf: boolean;
  toggling: boolean;
  activating: boolean;
  onPromoteDemote: () => void;
  onDeactivateReactivate: () => void;
  impersonateButton: React.ReactNode;
}

export function AccountRow({
  acc,
  isSelf,
  toggling,
  activating,
  onPromoteDemote,
  onDeactivateReactivate,
  impersonateButton,
}: AccountRowProps) {
  return (
    <tr className="border-b border-slate-50 last:border-0">
      <td className="px-4 py-2.5 text-gray-800">
        {acc.name || (
          <span className="text-gray-400 italic" title={acc.id}>
            {acc.id.slice(0, 8)}...
          </span>
        )}
        {isSelf && <span className="ml-1.5 text-xs text-gray-400">(you)</span>}
      </td>
      <td className="px-4 py-2.5 text-gray-500">{acc.email}</td>
      <td className="px-4 py-2.5">
        <div className="flex items-center gap-1.5">
          {acc.installation_admin ? (
            <span className="inline-flex items-center gap-1 text-xs font-medium text-amber-700 bg-amber-50 px-2 py-0.5 rounded">
              <Shield size={12} />
              Admin
            </span>
          ) : (
            <span className="text-xs text-gray-400">User</span>
          )}
          {acc.deactivated && (
            <span className="inline-flex items-center gap-1 text-xs font-medium text-red-700 bg-red-50 px-2 py-0.5 rounded">
              <Ban size={12} />
              Disabled
            </span>
          )}
        </div>
      </td>
      <td className="px-4 py-2.5 text-gray-400 text-xs whitespace-nowrap">{formatDate(acc.created_at)}</td>
      <td className="px-4 py-2.5 text-right">
        <div className="flex items-center justify-end gap-2">
          {!isSelf && impersonateButton}
          {isSelf ? (
            <Text className="text-xs text-gray-400">Cannot change own access</Text>
          ) : (
            <>
              <Button variant="outline" size="sm" onClick={onDeactivateReactivate} disabled={activating}>
                {activating ? (
                  "Updating..."
                ) : acc.deactivated ? (
                  <span className="flex items-center gap-1">
                    <RotateCcw size={14} />
                    Reactivate
                  </span>
                ) : (
                  <span className="flex items-center gap-1">
                    <Ban size={14} />
                    Deactivate
                  </span>
                )}
              </Button>
              <Button variant="outline" size="sm" onClick={onPromoteDemote} disabled={toggling}>
                {toggling ? (
                  "Updating..."
                ) : acc.installation_admin ? (
                  <span className="flex items-center gap-1">
                    <ShieldOff size={14} />
                    Demote
                  </span>
                ) : (
                  <span className="flex items-center gap-1">
                    <Shield size={14} />
                    Promote
                  </span>
                )}
              </Button>
            </>
          )}
        </div>
      </td>
    </tr>
  );
}
