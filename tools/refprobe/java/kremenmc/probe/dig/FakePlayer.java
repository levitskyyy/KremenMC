package kremenmc.probe.dig;

import java.util.*;
import com.mojang.authlib.GameProfile;
import io.netty.channel.embedded.EmbeddedChannel;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.PacketFlow;
import net.minecraft.server.level.ClientInformation;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.network.CommonListenerCookie;
import net.minecraft.server.network.ServerGamePacketListenerImpl;
import net.minecraft.util.Prediction;
import net.minecraft.world.entity.item.ItemEntity;
import net.minecraft.world.item.ItemStack;

/** A server player wired to an in-memory connection; records drops and sent packets. */
final class FakePlayer extends ServerPlayer {
    final List<String> drops = new ArrayList<>();
    final EmbeddedChannel channel;

    FakePlayer(ServerLevel level) {
        this(level, new GameProfile(UUID.randomUUID(), "probe"));
    }

    private FakePlayer(ServerLevel level, GameProfile profile) {
        super(level.getServer(), level, profile, ClientInformation.createDefault());
        net.minecraft.network.Connection conn = new net.minecraft.network.Connection(PacketFlow.SERVERBOUND);
        channel = new EmbeddedChannel(conn);
        connection = new ServerGamePacketListenerImpl(level.getServer(), conn, this, CommonListenerCookie.createInitial(profile, false));
        connection.handleAcceptPlayerLoad(new net.minecraft.network.protocol.game.ServerboundPlayerLoadedPacket());
    }

    @Override
    public ItemEntity drop(ItemStack stack, boolean throwRandomly, Prediction prediction) {
        if (!stack.isEmpty()) drops.add("x" + (throwRandomly ? ":t" : ":f"));
        return null;
    }

    /** Runs n ticks of item use (LivingEntity.updateUsingItem). */
    void tickUse(int n) {
        for (int i = 0; i < n && isUsingItem(); i++) updateUsingItem(getUseItem());
    }

    /** Packets sent to this player since the last call. */
    List<Packet<?>> sent() {
        List<Packet<?>> out = new ArrayList<>();
        Object o;
        while ((o = channel.readOutbound()) != null) {
            if (o instanceof Packet<?> p) out.add(p);
        }
        return out;
    }
}
