package kremenmc.bridge;

import java.lang.ref.Cleaner;
import java.lang.ref.WeakReference;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashMap;
import java.util.List;
import java.util.ListIterator;
import java.util.Map;
import java.util.Objects;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import net.kyori.adventure.text.Component;
import org.bukkit.Material;
import org.bukkit.entity.HumanEntity;
import org.bukkit.entity.Player;
import org.bukkit.event.inventory.ClickType;
import org.bukkit.event.inventory.InventoryAction;
import org.bukkit.event.inventory.InventoryClickEvent;
import org.bukkit.event.inventory.InventoryCloseEvent;
import org.bukkit.event.inventory.InventoryDragEvent;
import org.bukkit.event.inventory.InventoryOpenEvent;
import org.bukkit.event.inventory.InventoryType;
import org.bukkit.inventory.EntityEquipment;
import org.bukkit.inventory.EquipmentSlot;
import org.bukkit.inventory.Inventory;
import org.bukkit.inventory.InventoryHolder;
import org.bukkit.inventory.InventoryView;
import org.bukkit.inventory.ItemStack;
import org.bukkit.inventory.PlayerInventory;

/**
 * Inventories and inventory events (protocol in internal/bridge/inv.go). Go
 * owns the contents of player, ender chest and custom inventories; Java keeps
 * the plugin-facing objects (holder, title) and the open custom views.
 */
final class Inventories {
    private Inventories() {}

    static final int OP_CONTENTS = 60, OP_SET = 61, OP_SET_ALL = 62, OP_CREATE = 63, OP_FREE = 64, OP_OPEN = 65,
            OP_CLOSE = 66, OP_VIEW = 67, OP_HELD = 68, OP_ADD = 69, OP_EVENT = 70;

    private static final Cleaner CLEANER = Cleaner.create();
    private static final Map<Long, WeakReference<Inventory>> custom = new ConcurrentHashMap<>();
    private static final Map<UUID, InventoryView> open = new ConcurrentHashMap<>();
    private static final Map<UUID, PlayerInventory> playerInvs = new ConcurrentHashMap<>();
    private static final Map<UUID, Inventory> enders = new ConcurrentHashMap<>();

    static Inventory create(Bridge b, InventoryHolder holder, InventoryType type, int size, Component title) {
        if (size <= 0) size = type.getDefaultSize();
        if (title == null) title = type.defaultTitle();
        long id = Wire.num(b.call(OP_CREATE, type.name(), size, Text.json(title)), 0);
        InvImpl impl = new InvImpl(b, id, type, sizeFor(type, size), holder, title);
        Inventory inv = Stubs.proxy(Inventory.class, impl);
        custom.put(id, new WeakReference<>(inv));
        Conn conn = b.conn;
        CLEANER.register(inv, () -> {
            custom.remove(id);
            try {
                conn.notify(OP_FREE, id);
            } catch (RuntimeException ignored) {
            }
        });
        return inv;
    }

    private static int sizeFor(InventoryType type, int size) {
        return switch (type) {
            case HOPPER -> 5;
            case DISPENSER, DROPPER -> 9;
            case SHULKER_BOX, ENDER_CHEST, BARREL -> 27;
            default -> Math.min(Math.max((size + 8) / 9, 1), 6) * 9;
        };
    }

    static PlayerInventory playerInventory(Bridge b, UUID id) {
        return playerInvs.computeIfAbsent(id, k -> Stubs.proxy(PlayerInventory.class, new PlayerInvImpl(b, k)));
    }

    static EntityEquipment equipment(Bridge b, UUID id) {
        return Stubs.proxy(EntityEquipment.class, Stubs.implOf(playerInventory(b, id)));
    }

    static Inventory enderChest(Bridge b, UUID id) {
        return enders.computeIfAbsent(id, k -> Stubs.proxy(Inventory.class,
                new InvImpl(b, "ender:" + k, InventoryType.ENDER_CHEST, 27, null, InventoryType.ENDER_CHEST.defaultTitle())));
    }

    static void forget(UUID id) {
        open.remove(id);
        playerInvs.remove(id);
        enders.remove(id);
    }

    // ---- inventories ----

    /** A Go-backed inventory, or a local one when ref is null. */
    public static class InvImpl {
        final Bridge b;
        final Object ref;
        final InventoryType type;
        final int size;
        final InventoryHolder holder;
        final Component title;
        final ItemStack[] local;
        int maxStack = 99;

        InvImpl(Bridge b, Object ref, InventoryType type, int size, InventoryHolder holder, Component title) {
            this.b = b;
            this.ref = ref;
            this.type = type;
            this.size = size;
            this.holder = holder;
            this.title = title;
            this.local = ref == null ? new ItemStack[size] : null;
        }

        List<Object> raw() {
            List<Object> r = b.call(OP_CONTENTS, ref);
            return r.isEmpty() ? List.of() : Wire.list(r.get(0));
        }

        public int getSize() { return size; }
        public int getMaxStackSize() { return maxStack; }
        public void setMaxStackSize(int n) { maxStack = n; }
        public InventoryType getType() { return type; }
        public InventoryHolder getHolder() { return holder; }
        public InventoryHolder getHolder(boolean snapshot) { return getHolder(); }
        public org.bukkit.Location getLocation() { return null; }

        public ItemStack getItem(int slot) {
            if (slot < 0 || slot >= size) return null;
            if (local != null) return local[slot];
            List<Object> r = raw();
            return slot < r.size() ? Items.fromWireOrNull(r.get(slot)) : null;
        }

        public void setItem(int slot, ItemStack s) {
            if (slot < 0 || slot >= size) throw new ArrayIndexOutOfBoundsException(slot);
            if (local != null) {
                local[slot] = s;
                return;
            }
            b.conn.notify(OP_SET, ref, slot, Items.wire(s));
        }

        public ItemStack[] getContents() {
            ItemStack[] out = new ItemStack[size];
            if (local != null) return local.clone();
            List<Object> r = raw();
            for (int i = 0; i < size && i < r.size(); i++) out[i] = Items.fromWireOrNull(r.get(i));
            return out;
        }

        public void setContents(ItemStack[] items) {
            if (items.length > size) throw new IllegalArgumentException("Invalid inventory size (" + items.length + "); expected " + size + " or less");
            ItemStack[] all = Arrays.copyOf(items, size);
            if (local != null) {
                System.arraycopy(all, 0, local, 0, size);
                return;
            }
            b.conn.notify(OP_SET_ALL, ref, Items.wire(all));
        }

        public ItemStack[] getStorageContents() { return getContents(); }
        public void setStorageContents(ItemStack[] items) { setContents(items); }

        public HashMap<Integer, ItemStack> addItem(ItemStack... items) {
            HashMap<Integer, ItemStack> left = new HashMap<>();
            for (int i = 0; i < items.length; i++) {
                ItemStack s = Objects.requireNonNull(items[i], "Item cannot be null");
                if (local != null) {
                    ItemStack rest = addLocal(s.clone());
                    if (rest != null) left.put(i, rest);
                    continue;
                }
                List<Object> r = Wire.list(b.call(OP_ADD, ref, List.of(Items.wire(s))).get(0));
                if (!r.isEmpty() && r.getFirst() != null) left.put(i, Items.fromWire(r.getFirst()));
            }
            return left;
        }

        private ItemStack addLocal(ItemStack s) {
            for (int i = 0; i < size && s.getAmount() > 0; i++) {
                ItemStack c = local[i];
                if (c != null && c.isSimilar(s)) {
                    int n = Math.min(c.getMaxStackSize() - c.getAmount(), s.getAmount());
                    if (n > 0) {
                        c.setAmount(c.getAmount() + n);
                        s.setAmount(s.getAmount() - n);
                    }
                }
            }
            for (int i = 0; i < size && s.getAmount() > 0; i++) {
                if (local[i] == null || local[i].isEmpty()) {
                    int n = Math.min(s.getMaxStackSize(), s.getAmount());
                    local[i] = s.asQuantity(n);
                    s.setAmount(s.getAmount() - n);
                }
            }
            return s.getAmount() > 0 ? s : null;
        }

        public HashMap<Integer, ItemStack> removeItem(ItemStack... items) {
            ItemStack[] c = getContents();
            HashMap<Integer, ItemStack> left = new HashMap<>();
            for (int i = 0; i < items.length; i++) {
                ItemStack want = items[i];
                int n = want.getAmount();
                for (int k = 0; k < c.length && n > 0; k++) {
                    if (c[k] == null || !c[k].isSimilar(want)) continue;
                    int take = Math.min(n, c[k].getAmount());
                    n -= take;
                    c[k] = c[k].getAmount() == take ? null : c[k].asQuantity(c[k].getAmount() - take);
                }
                if (n > 0) left.put(i, want.asQuantity(n));
            }
            setContents(c);
            return left;
        }

        public HashMap<Integer, ItemStack> removeItemAnySlot(ItemStack... items) { return removeItem(items); }

        private static boolean empty(ItemStack s) { return s == null || s.isEmpty(); }

        public boolean contains(Material m) { return first(m) >= 0; }
        public boolean contains(ItemStack s) {
            if (s == null) return false;
            for (ItemStack c : getContents()) if (s.equals(c)) return true;
            return false;
        }
        public boolean contains(Material m, int amount) {
            int n = 0;
            for (ItemStack c : getContents()) if (!empty(c) && c.getType() == m) n += c.getAmount();
            return n >= amount;
        }
        public boolean contains(ItemStack s, int amount) {
            int n = 0;
            for (ItemStack c : getContents()) if (s != null && s.equals(c)) n++;
            return n >= amount;
        }
        public boolean containsAtLeast(ItemStack s, int amount) {
            int n = 0;
            for (ItemStack c : getContents()) if (s != null && !empty(c) && c.isSimilar(s)) n += c.getAmount();
            return n >= amount;
        }
        public HashMap<Integer, ItemStack> all(Material m) {
            HashMap<Integer, ItemStack> out = new HashMap<>();
            ItemStack[] c = getContents();
            for (int i = 0; i < c.length; i++) if (!empty(c[i]) && c[i].getType() == m) out.put(i, c[i]);
            return out;
        }
        public HashMap<Integer, ItemStack> all(ItemStack s) {
            HashMap<Integer, ItemStack> out = new HashMap<>();
            ItemStack[] c = getContents();
            for (int i = 0; i < c.length; i++) if (s != null && s.equals(c[i])) out.put(i, c[i]);
            return out;
        }
        public int first(Material m) {
            ItemStack[] c = getContents();
            for (int i = 0; i < c.length; i++) if (!empty(c[i]) && c[i].getType() == m) return i;
            return -1;
        }
        public int first(ItemStack s) {
            ItemStack[] c = getContents();
            for (int i = 0; i < c.length; i++) if (s != null && s.equals(c[i])) return i;
            return -1;
        }
        public int firstEmpty() {
            ItemStack[] c = getStorageContents();
            for (int i = 0; i < c.length; i++) if (empty(c[i])) return i;
            return -1;
        }
        public boolean isEmpty() {
            for (ItemStack c : getContents()) if (!empty(c)) return false;
            return true;
        }
        public void remove(Material m) {
            ItemStack[] c = getContents();
            for (int i = 0; i < c.length; i++) if (!empty(c[i]) && c[i].getType() == m) c[i] = null;
            setContents(c);
        }
        public void remove(ItemStack s) {
            ItemStack[] c = getContents();
            for (int i = 0; i < c.length; i++) if (s != null && s.equals(c[i])) c[i] = null;
            setContents(c);
        }
        public void clear(int slot) { setItem(slot, null); }
        public void clear() { setContents(new ItemStack[size]); }

        public List<HumanEntity> getViewers() {
            List<HumanEntity> out = new ArrayList<>();
            for (Map.Entry<UUID, InventoryView> e : open.entrySet()) {
                if (Stubs.implOf(e.getValue().getTopInventory()) == this) out.add(e.getValue().getPlayer());
            }
            return out;
        }

        public int close() {
            List<HumanEntity> v = getViewers();
            for (HumanEntity h : v) h.closeInventory();
            return v.size();
        }

        public ListIterator<ItemStack> iterator() { return iterator(0); }
        public ListIterator<ItemStack> iterator(int index) {
            ItemStack[] c = getContents();
            List<ItemStack> l = new ArrayList<>(Arrays.asList(c));
            InvImpl me = this;
            ListIterator<ItemStack> it = l.listIterator(index < 0 ? size + index : index);
            return new ListIterator<>() {
                int last = -1;
                @Override public boolean hasNext() { return it.hasNext(); }
                @Override public ItemStack next() { last = it.nextIndex(); return it.next(); }
                @Override public boolean hasPrevious() { return it.hasPrevious(); }
                @Override public ItemStack previous() { last = it.previousIndex(); return it.previous(); }
                @Override public int nextIndex() { return it.nextIndex(); }
                @Override public int previousIndex() { return it.previousIndex(); }
                @Override public void remove() { throw new UnsupportedOperationException(); }
                @Override public void set(ItemStack s) { it.set(s); me.setItem(last, s); }
                @Override public void add(ItemStack s) { throw new UnsupportedOperationException(); }
            };
        }

        @Override public String toString() { return "Inventory{" + type + ", size=" + size + ", ref=" + ref + "}"; }
    }

    /** The player's inventory: Bukkit indices 0-8 hotbar, 9-35 main, 36-39 boots..helmet, 40 off hand. */
    public static final class PlayerInvImpl extends InvImpl {
        final UUID id;

        PlayerInvImpl(Bridge b, UUID id) {
            super(b, id, InventoryType.PLAYER, 41, null, InventoryType.PLAYER.defaultTitle());
            this.id = id;
        }

        @Override public InventoryHolder getHolder() { return b.players.cached(id); }

        @Override public ItemStack[] getStorageContents() { return Arrays.copyOf(getContents(), 36); }
        @Override public void setStorageContents(ItemStack[] items) {
            ItemStack[] c = getContents();
            System.arraycopy(Arrays.copyOf(items, 36), 0, c, 0, 36);
            setContents(c);
        }
        public ItemStack[] getArmorContents() { return Arrays.copyOfRange(getContents(), 36, 40); }
        public void setArmorContents(ItemStack[] a) { for (int i = 0; i < 4; i++) setItem(36 + i, i < a.length ? a[i] : null); }
        public ItemStack[] getExtraContents() { return new ItemStack[] {getItem(40)}; }
        public void setExtraContents(ItemStack[] a) { setItem(40, a.length > 0 ? a[0] : null); }
        private ItemStack orAir(ItemStack s) { return s == null ? Items.empty() : s; }
        public ItemStack getHelmet() { return getItem(39); }
        public ItemStack getChestplate() { return getItem(38); }
        public ItemStack getLeggings() { return getItem(37); }
        public ItemStack getBoots() { return getItem(36); }
        public void setHelmet(ItemStack s) { setItem(39, s); }
        public void setChestplate(ItemStack s) { setItem(38, s); }
        public void setLeggings(ItemStack s) { setItem(37, s); }
        public void setBoots(ItemStack s) { setItem(36, s); }
        public void setHelmet(ItemStack s, boolean silent) { setItem(39, s); }
        public void setChestplate(ItemStack s, boolean silent) { setItem(38, s); }
        public void setLeggings(ItemStack s, boolean silent) { setItem(37, s); }
        public void setBoots(ItemStack s, boolean silent) { setItem(36, s); }

        public int getHeldItemSlot() {
            List<Object> r = b.call(OP_CONTENTS, ref);
            return (int) Wire.num(r, 1);
        }
        public void setHeldItemSlot(int slot) {
            if (slot < 0 || slot > 8) throw new IllegalArgumentException("Slot is not between 0 and 8 inclusive");
            b.conn.notify(OP_HELD, id, slot);
        }
        public ItemStack getItemInMainHand() { return orAir(getItem(getHeldItemSlot())); }
        public void setItemInMainHand(ItemStack s) { setItem(getHeldItemSlot(), s); }
        public void setItemInMainHand(ItemStack s, boolean silent) { setItemInMainHand(s); }
        public ItemStack getItemInOffHand() { return orAir(getItem(40)); }
        public void setItemInOffHand(ItemStack s) { setItem(40, s); }
        public void setItemInOffHand(ItemStack s, boolean silent) { setItem(40, s); }
        public ItemStack getItemInHand() { return getItemInMainHand(); }
        public void setItemInHand(ItemStack s) { setItemInMainHand(s); }

        static int slotOf(EquipmentSlot e, int held) {
            return switch (e) {
                case HAND -> held;
                case OFF_HAND -> 40;
                case FEET -> 36;
                case LEGS -> 37;
                case CHEST -> 38;
                case HEAD -> 39;
                default -> -1;
            };
        }
        public ItemStack getItem(EquipmentSlot e) {
            int s = slotOf(e, e == EquipmentSlot.HAND ? getHeldItemSlot() : 0);
            return s < 0 ? Items.empty() : orAir(getItem(s));
        }
        public void setItem(EquipmentSlot e, ItemStack it) {
            int s = slotOf(e, e == EquipmentSlot.HAND ? getHeldItemSlot() : 0);
            if (s >= 0) setItem(s, it);
        }
        public void setItem(EquipmentSlot e, ItemStack it, boolean silent) { setItem(e, it); }

        ItemStack cursor() {
            List<Object> r = b.call(OP_CONTENTS, ref);
            return r.size() > 2 ? Items.fromWire(r.get(2)) : Items.empty();
        }
        void cursor(ItemStack s) { b.conn.notify(OP_SET, ref, -1, Items.wire(s)); }

        @Override public String toString() { return "PlayerInventory{" + id + "}"; }
    }

    // ---- views ----

    static InventoryView view(Bridge b, Player p, Inventory top) {
        return Stubs.proxy(InventoryView.class, new ViewImpl(b, p, top));
    }

    static Inventory crafting(Bridge b) {
        return Stubs.proxy(Inventory.class, new InvImpl(b, null, InventoryType.CRAFTING, 5, null, InventoryType.CRAFTING.defaultTitle()));
    }

    public static final class ViewImpl {
        final Bridge b;
        final Player player;
        final Inventory top;
        final PlayerInventory bottom;
        final boolean crafting;
        String titleOverride;

        ViewImpl(Bridge b, Player player, Inventory top) {
            this.b = b;
            this.player = player;
            this.top = top;
            this.bottom = playerInventory(b, player.getUniqueId());
            this.crafting = top.getType() == InventoryType.CRAFTING;
        }

        private PlayerInvImpl pinv() { return (PlayerInvImpl) Stubs.implOf(bottom); }

        public Inventory getTopInventory() { return top; }
        public Inventory getBottomInventory() { return bottom; }
        public HumanEntity getPlayer() { return player; }
        public InventoryType getType() { return top.getType(); }
        public int countSlots() { return top.getSize() + (crafting ? 41 : 36); }

        public int convertSlot(int raw) {
            int n = top.getSize();
            if (raw < n) return raw;
            if (crafting) {
                if (raw >= 5 && raw <= 8) return 39 - (raw - 5);
                if (raw >= 36 && raw <= 44) return raw - 36;
                if (raw == 45) return 40;
                return raw;
            }
            int i = raw - n;
            return i < 27 ? 9 + i : i - 27;
        }

        public Inventory getInventory(int raw) {
            if (raw == InventoryView.OUTSIDE || raw == -1 || raw < 0) return null;
            if (raw < top.getSize()) return top;
            return raw < countSlots() || crafting ? bottom : null;
        }

        public InventoryType.SlotType getSlotType(int raw) {
            if (raw < 0) return InventoryType.SlotType.OUTSIDE;
            if (crafting) {
                if (raw == 0) return InventoryType.SlotType.RESULT;
                if (raw < 5) return InventoryType.SlotType.CRAFTING;
                if (raw < 9) return InventoryType.SlotType.ARMOR;
                if (raw >= 36 && raw <= 44) return InventoryType.SlotType.QUICKBAR;
                return InventoryType.SlotType.CONTAINER;
            }
            if (raw < top.getSize()) return InventoryType.SlotType.CONTAINER;
            return convertSlot(raw) < 9 ? InventoryType.SlotType.QUICKBAR : InventoryType.SlotType.CONTAINER;
        }

        public ItemStack getItem(int raw) {
            Inventory inv = getInventory(raw);
            return inv == null ? null : inv.getItem(convertSlot(raw));
        }

        public void setItem(int raw, ItemStack s) {
            Inventory inv = getInventory(raw);
            if (inv != null) inv.setItem(convertSlot(raw), s);
        }

        public ItemStack getCursor() { return pinv().cursor(); }
        public void setCursor(ItemStack s) { pinv().cursor(s); }
        public void open() { player.openInventory(top); }
        public void close() { player.closeInventory(); }
        public boolean setProperty(InventoryView.Property p, int v) { return false; }

        public Component title() {
            if (titleOverride != null) return Text.component(titleOverride);
            return Stubs.implOf(top) instanceof InvImpl i ? i.title : getType().defaultTitle();
        }
        public String getTitle() { return Text.legacy(title()); }
        public String getOriginalTitle() { return Stubs.implOf(top) instanceof InvImpl i ? Text.legacy(i.title) : getType().getDefaultTitle(); }
        public void setTitle(String t) { titleOverride = Text.jsonLegacy(t); }
        public org.bukkit.inventory.MenuType getMenuType() { return null; }
        @Override public String toString() { return "InventoryView{" + player.getName() + ", " + top + "}"; }
    }

    // ---- player hooks ----

    static InventoryView openView(Bridge b, Player p) {
        InventoryView v = open.get(p.getUniqueId());
        return v != null ? v : view(b, p, crafting(b));
    }

    static InventoryView openInventory(Bridge b, Player p, Inventory inv) {
        if (inv == null) return null;
        if (!(Stubs.implOf(inv) instanceof InvImpl impl) || impl.ref == null || impl instanceof PlayerInvImpl) return null;
        InventoryView old = open.remove(p.getUniqueId());
        if (old != null) b.pluginManager.callEvent(new InventoryCloseEvent(old, InventoryCloseEvent.Reason.OPEN_NEW));
        InventoryView v = view(b, p, inv);
        InventoryOpenEvent e = new InventoryOpenEvent(v);
        b.pluginManager.callEvent(e);
        if (e.isCancelled()) {
            if (old != null) b.conn.notify(OP_CLOSE, p.getUniqueId());
            return null;
        }
        open.put(p.getUniqueId(), v);
        b.conn.notify(OP_OPEN, p.getUniqueId(), impl.ref);
        return v;
    }

    static void closeInventory(Bridge b, Player p, InventoryCloseEvent.Reason reason) {
        InventoryView old = open.remove(p.getUniqueId());
        if (old != null) b.pluginManager.callEvent(new InventoryCloseEvent(old, reason));
        b.conn.notify(OP_CLOSE, p.getUniqueId());
    }

    // ---- events from Go ----

    static Object[] event(Bridge b, String name, Map<String, Object> f) throws Exception {
        return b.main.call(() -> dispatch(b, name, f), 30_000);
    }

    private static Inventory top(Bridge b, Map<String, Object> f) {
        Object ref = f.get("ref");
        if (ref instanceof Long id) {
            WeakReference<Inventory> w = custom.get(id);
            Inventory inv = w == null ? null : w.get();
            if (inv != null) return inv;
        }
        InventoryType t;
        try {
            t = InventoryType.valueOf(Wire.str(f, "type"));
        } catch (IllegalArgumentException e) {
            t = InventoryType.CHEST;
        }
        if (t == InventoryType.CRAFTING) return crafting(b);
        Component title = Text.component(Wire.str(f, "title"));
        return Stubs.proxy(Inventory.class, new InvImpl(b, ref, t, (int) Wire.lng(f, "size"), null, title == null ? t.defaultTitle() : title));
    }

    private static InventoryView eventView(Bridge b, Player p, Map<String, Object> f) {
        Inventory top = top(b, f);
        InventoryView cur = open.get(p.getUniqueId());
        if (cur != null && cur.getTopInventory() == top) return cur;
        return view(b, p, top);
    }

    private static Object[] dispatch(Bridge b, String name, Map<String, Object> f) {
        UUID id = (UUID) f.get("player");
        Player p = b.players.cached(id);
        if (p == null) p = b.players.get(id);
        if (p == null) return new Object[] {false, Map.of()};
        InventoryView v = eventView(b, p, f);
        switch (name) {
            case "InventoryClick": {
                int raw = (int) Wire.lng(f, "slot");
                ClickType click;
                try {
                    click = ClickType.valueOf(Wire.str(f, "click"));
                } catch (IllegalArgumentException e) {
                    click = ClickType.UNKNOWN;
                }
                ItemStack cur = Items.fromWireOrNull(f.get("current"));
                ItemStack cursor = Items.fromWireOrNull(f.get("cursor"));
                InventoryAction action = action(click, raw, cur, cursor, f.get("hotbarItem") != null);
                InventoryType.SlotType st = raw == -999 ? InventoryType.SlotType.OUTSIDE : v.getSlotType(raw);
                InventoryClickEvent e = click == ClickType.NUMBER_KEY
                        ? new InventoryClickEvent(v, st, raw, click, action, (int) Wire.lng(f, "hotbar"))
                        : new InventoryClickEvent(v, st, raw, click, action);
                if (raw == -999) e.setCurrentItem(cur);
                b.pluginManager.callEvent(e);
                return new Object[] {e.isCancelled(), Map.of()};
            }
            case "InventoryDrag": {
                Map<Integer, ItemStack> items = new HashMap<>();
                List<Object> slots = Wire.list(f.get("slots")), its = Wire.list(f.get("items"));
                for (int i = 0; i < slots.size() && i < its.size(); i++) {
                    items.put(((Number) slots.get(i)).intValue(), Items.fromWire(its.get(i)));
                }
                ItemStack newCursor = Items.fromWireOrNull(f.get("newCursor"));
                InventoryDragEvent e = new InventoryDragEvent(v, newCursor, Items.fromWire(f.get("cursor")), Wire.bool(f, "right"), items);
                b.pluginManager.callEvent(e);
                return new Object[] {e.isCancelled(), Map.of()};
            }
            case "InventoryOpen": {
                InventoryOpenEvent e = new InventoryOpenEvent(v);
                b.pluginManager.callEvent(e);
                return new Object[] {e.isCancelled(), Map.of()};
            }
            case "InventoryClose": {
                open.remove(id);
                InventoryCloseEvent.Reason r;
                try {
                    r = InventoryCloseEvent.Reason.valueOf(Wire.str(f, "reason"));
                } catch (IllegalArgumentException e) {
                    r = InventoryCloseEvent.Reason.PLAYER;
                }
                b.pluginManager.callEvent(new InventoryCloseEvent(v, r));
                return new Object[] {false, Map.of()};
            }
            default:
                throw new IllegalArgumentException("unknown inventory event " + name);
        }
    }

    private static boolean empty(ItemStack s) { return s == null || s.isEmpty(); }

    private static InventoryAction action(ClickType click, int raw, ItemStack cur, ItemStack cursor, boolean hotbarFull) {
        switch (click) {
            case LEFT, RIGHT -> {
                if (raw == -999) {
                    if (empty(cursor)) return InventoryAction.NOTHING;
                    return click == ClickType.LEFT ? InventoryAction.DROP_ALL_CURSOR : InventoryAction.DROP_ONE_CURSOR;
                }
                if (empty(cur)) {
                    if (empty(cursor)) return InventoryAction.NOTHING;
                    return click == ClickType.LEFT || cursor.getAmount() == 1 ? InventoryAction.PLACE_ALL : InventoryAction.PLACE_ONE;
                }
                if (empty(cursor)) return click == ClickType.LEFT ? InventoryAction.PICKUP_ALL : InventoryAction.PICKUP_HALF;
                if (cur.isSimilar(cursor)) return click == ClickType.LEFT ? InventoryAction.PLACE_ALL : InventoryAction.PLACE_ONE;
                return InventoryAction.SWAP_WITH_CURSOR;
            }
            case SHIFT_LEFT, SHIFT_RIGHT -> { return empty(cur) ? InventoryAction.NOTHING : InventoryAction.MOVE_TO_OTHER_INVENTORY; }
            case NUMBER_KEY -> { return empty(cur) && !hotbarFull ? InventoryAction.NOTHING : empty(cur) ? InventoryAction.HOTBAR_SWAP : InventoryAction.HOTBAR_MOVE_AND_READD; }
            case SWAP_OFFHAND -> { return InventoryAction.HOTBAR_SWAP; }
            case MIDDLE -> { return empty(cur) ? InventoryAction.NOTHING : InventoryAction.CLONE_STACK; }
            case DROP -> { return empty(cur) ? InventoryAction.NOTHING : InventoryAction.DROP_ONE_SLOT; }
            case CONTROL_DROP -> { return empty(cur) ? InventoryAction.NOTHING : InventoryAction.DROP_ALL_SLOT; }
            case DOUBLE_CLICK -> { return InventoryAction.COLLECT_TO_CURSOR; }
            case WINDOW_BORDER_LEFT, WINDOW_BORDER_RIGHT -> { return InventoryAction.NOTHING; }
            default -> { return InventoryAction.UNKNOWN; }
        }
    }
}
