package kremenmc.probe.item;

import java.nio.file.*;
import java.util.*;
import com.mojang.brigadier.StringReader;
import com.mojang.serialization.DynamicOps;
import com.google.common.hash.HashCode;
import net.minecraft.core.Registry;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.core.component.*;
import net.minecraft.commands.arguments.item.ItemParser;
import net.minecraft.network.HashedPatchMap;
import net.minecraft.network.HashedStack;
import net.minecraft.network.RegistryFriendlyByteBuf;
import net.minecraft.network.codec.StreamCodec;
import net.minecraft.util.HashOps;
import net.minecraft.world.item.*;

/**
 * Component corpus: per-component stream encodings and hashes for every
 * prototype value, every creative-tab stack and every sample in the file
 * given as args[0] (one /give-syntax item per line).
 */
public final class Components {
    static DynamicOps<HashCode> ops;
    static HashedPatchMap.HashGenerator gen;
    static final Set<String> seen = new HashSet<>();

    @SuppressWarnings({"unchecked", "rawtypes"})
    static String enc(DataComponentType t, Object v) {
        RegistryFriendlyByteBuf b = Ref.buf();
        ((StreamCodec) t.streamCodec()).encode(b, v);
        return Ref.hex(b);
    }

    @SuppressWarnings({"unchecked", "rawtypes"})
    static String hash(DataComponentType t, Object v) {
        try {
            return Integer.toString(gen.apply(new TypedDataComponent(t, v)));
        } catch (Exception e) {
            return "-";
        }
    }

    static String name(DataComponentType<?> t) {
        return BuiltInRegistries.DATA_COMPONENT_TYPE.getKey(t).toString();
    }

    static void comp(DataComponentType<?> t, Object v, String src) {
        if (t.isTransient() || t.streamCodec() == null) return;
        String h = enc(t, v);
        if (!seen.add(name(t) + " " + h)) return;
        Ref.out.println("comp " + name(t) + " " + h + " " + hash(t, v) + " " + src);
    }

    static void stack(ItemStack s, String src) {
        RegistryFriendlyByteBuf b = Ref.buf();
        ItemStack.OPTIONAL_STREAM_CODEC.encode(b, s);
        String h = Ref.hex(b);
        if (!seen.add("stack " + h)) return;
        RegistryFriendlyByteBuf hb = Ref.buf();
        HashedStack.STREAM_CODEC.encode(hb, HashedStack.create(s, gen));
        Ref.out.println("stack " + h + " " + Ref.hex(hb) + " " + src);
        for (TypedDataComponent<?> c : s.getComponentsPatch().split().added()) comp(c.type(), c.value(), src);
    }

    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        var reg = server.registryAccess();
        ops = reg.createSerializationContext(HashOps.CRC32C_INSTANCE);
        gen = c -> c.encodeValue(ops).getOrThrow().asInt();

        for (var t : BuiltInRegistries.DATA_COMPONENT_TYPE) {
            Ref.out.println("ctype " + BuiltInRegistries.DATA_COMPONENT_TYPE.getId(t) + " " + name(t) + " " + (t.isTransient() ? 1 : 0) + " " + (t.streamCodec() == null ? 0 : 1));
        }
        for (var key : List.of(Registries.ENCHANTMENT, Registries.TRIM_MATERIAL, Registries.TRIM_PATTERN, Registries.JUKEBOX_SONG,
                Registries.INSTRUMENT, Registries.BANNER_PATTERN, Registries.PAINTING_VARIANT, Registries.DAMAGE_TYPE,
                Registries.WOLF_VARIANT, Registries.WOLF_SOUND_VARIANT, Registries.CAT_VARIANT, Registries.PIG_VARIANT,
                Registries.COW_VARIANT, Registries.CHICKEN_VARIANT, Registries.FROG_VARIANT)) {
            @SuppressWarnings({"unchecked", "rawtypes"})
            Registry<Object> r = (Registry) reg.lookupOrThrow(key);
            for (var e : r.entrySet()) {
                Ref.out.println("reg " + key.identifier() + " " + r.getIdOrThrow(e.getValue()) + " " + e.getKey().identifier());
            }
        }
        for (Item it : BuiltInRegistries.ITEM) {
            String src = BuiltInRegistries.ITEM.getKey(it).toString();
            for (TypedDataComponent<?> c : it.components()) comp(c.type(), c.value(), "proto:" + src);
        }
        CreativeModeTabs.tryRebuildTabContents(server.getWorldData().enabledFeatures(), true, reg);
        for (CreativeModeTab tab : CreativeModeTabs.allTabs()) {
            for (ItemStack s : tab.getDisplayItems()) stack(s, "creative:" + BuiltInRegistries.ITEM.getKey(s.getItem()));
        }
        if (args.length > 0) {
            ItemParser parser = new ItemParser(reg);
            int n = 0;
            for (String line : Files.readAllLines(Path.of(args[0]))) {
                n++;
                line = line.strip();
                if (line.isEmpty() || line.startsWith("#")) continue;
                String[] parts = line.split(" ", 2);
                int count = Integer.parseInt(parts[0]);
                try {
                    var in = parser.parse(new StringReader(parts[1]));
                    stack(in.createItemStack(count), "sample:" + n);
                } catch (Exception e) {
                    Ref.out.println("error " + n + " " + e.getMessage().replace('\n', ' '));
                }
            }
        }
        Ref.exit();
    }
}
