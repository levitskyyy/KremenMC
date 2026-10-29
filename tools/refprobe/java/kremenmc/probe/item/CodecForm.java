package kremenmc.probe.item;

import java.nio.file.*;
import net.minecraft.core.component.DataComponentType;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.nbt.NbtOps;
import net.minecraft.network.codec.StreamCodec;
import net.minecraft.resources.Identifier;
import com.mojang.serialization.JsonOps;

/** Dev aid: prints the codec form (JSON) of "component hex" lines from args[0]. */
public final class CodecForm {
    @SuppressWarnings({"unchecked", "rawtypes"})
    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        var ops = server.registryAccess().createSerializationContext(JsonOps.INSTANCE);
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            String[] f = line.strip().split(" ");
            if (f.length < 2) continue;
            DataComponentType t = BuiltInRegistries.DATA_COMPONENT_TYPE.getValue(Identifier.parse(f[0]));
            Object v = ((StreamCodec) t.streamCodec()).decode(Ref.buf(Ref.unhex(f[1])));
            Ref.out.println(f[0] + " " + t.codec().encodeStart(ops, v).getOrThrow());
        }
        Ref.exit();
    }
}
