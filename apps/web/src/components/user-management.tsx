"use client";

import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useSession } from "./providers";
import { users, managedRoles, type ManagedUser } from "@/lib/users";
import type { Role } from "@/lib/session";
import { Panel, ErrorState, LoadingState } from "./common";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "./ui/dialog";

function UserRow({ user }: { user: ManagedUser }) {
  const client = useQueryClient();
  const [role, setRole] = useState<Role>(user.role);
  const [active, setActive] = useState(user.active);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const changed = role !== user.role || active !== user.active;
  const save = async () => {
    setSaving(true);
    setError("");
    try {
      await users.update(user.id, { role, active });
      await client.invalidateQueries({ queryKey: ["managed-users"] });
      toast.success(`Updated ${user.username}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to update user");
    } finally {
      setSaving(false);
    }
  };
  return (
    <TableRow>
      <TableCell>
        <strong>{user.username}</strong>
        <div className="mono muted">{user.id}</div>
      </TableCell>
      <TableCell>
        <NativeSelect aria-label={`Role for ${user.username}`} value={role} onChange={(event) => setRole(event.target.value as Role)} disabled={saving}>
          {managedRoles.map((value) => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}
        </NativeSelect>
      </TableCell>
      <TableCell>
        <Label className="flex items-center gap-2">
          <input type="checkbox" checked={active} onChange={(event) => setActive(event.target.checked)} disabled={saving} />
          Active
        </Label>
      </TableCell>
      <TableCell>
        <Button size="sm" variant="outline" disabled={!changed || saving} onClick={() => void save()}>
          {saving ? "Saving…" : "Save changes"}
        </Button>
        {error && <p role="alert" className="text-destructive">{error}</p>}
      </TableCell>
    </TableRow>
  );
}

export function UserManagement() {
  const { session } = useSession();
  const admin = session.kind === "authenticated" && session.user.role === "Administrator";
  const [cursors, setCursors] = useState([""]);
  const cursor = cursors.at(-1) ?? "";
  const [createOpen, setCreateOpen] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("Viewer");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ["managed-users", cursor],
    queryFn: () => users.list(cursor),
    enabled: admin,
    refetchOnWindowFocus: false,
  });

  if (!admin) {
    return <Panel title="Operator accounts" description="Administrator access is required to manage users and roles.">
      <p className="panel-padding muted">Sign in as an Administrator in protected mode to review and manage local operator accounts.</p>
    </Panel>;
  }

  const create = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      await users.create({ username: username.trim(), password, role });
      setPassword("");
      setUsername("");
      setRole("Viewer");
      setCreateOpen(false);
      setCursors([""]);
      await client.invalidateQueries({ queryKey: ["managed-users"] });
      toast.success("Operator account created");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to create account");
    } finally {
      setSaving(false);
    }
  };

  return <>
    <Panel title="Operator accounts" description="Local accounts · role and activation changes take effect on the next request." action={<Button onClick={() => { setError(""); setCreateOpen(true); }}>Create user</Button>}>
      {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => query.refetch()} /></div> : <>
        <Table>
          <TableHeader><TableRow><TableHead>Account</TableHead><TableHead>Role</TableHead><TableHead>Status</TableHead><TableHead>Action</TableHead></TableRow></TableHeader>
          <TableBody>{query.data.items.map((user) => <UserRow key={user.id} user={user} />)}</TableBody>
        </Table>
        <div className="page-actions panel-padding">
          <Badge variant="outline">Page {cursors.length}</Badge>
          <Button variant="outline" disabled={cursors.length === 1} onClick={() => setCursors((current) => current.slice(0, -1))}>Previous</Button>
          <Button variant="outline" disabled={!query.data.more || !query.data.next} onClick={() => setCursors((current) => [...current, query.data.next ?? ""])}>Next</Button>
        </div>
      </>}
    </Panel>
    <Dialog open={createOpen} onOpenChange={(open) => { if (!saving) setCreateOpen(open); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create operator account</DialogTitle>
          <DialogDescription>Set a unique username, initial password and role. Share the password through an approved private channel.</DialogDescription>
        </DialogHeader>
        <form onSubmit={(event) => void create(event)} className="incident-edit-form">
          <div><Label htmlFor="new-username">Username</Label><Input id="new-username" value={username} onChange={(event) => setUsername(event.target.value)} minLength={3} maxLength={60} pattern="[a-z][a-z0-9._-]{2,59}" autoComplete="off" required disabled={saving} /></div>
          <div><Label htmlFor="new-password">Initial password</Label><Input id="new-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} minLength={16} maxLength={72} autoComplete="new-password" required disabled={saving} /><p className="muted">16–72 characters; no leading or trailing spaces.</p></div>
          <div><Label htmlFor="new-role">Role</Label><NativeSelect id="new-role" value={role} onChange={(event) => setRole(event.target.value as Role)} disabled={saving}>{managedRoles.map((value) => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}</NativeSelect></div>
          {error && <p role="alert" className="text-destructive">{error}</p>}
          <div className="page-actions"><Button type="button" variant="outline" disabled={saving} onClick={() => setCreateOpen(false)}>Cancel</Button><Button type="submit" disabled={saving}>{saving ? "Creating…" : "Create user"}</Button></div>
        </form>
      </DialogContent>
    </Dialog>
  </>;
}
