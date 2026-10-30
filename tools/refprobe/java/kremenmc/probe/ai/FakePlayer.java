package kremenmc.probe.ai;

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

/** A server player wired to an in-memory connection; records sent packets. */
final class FakePlayer extends ServerPlayer {
    final EmbeddedChannel channel;

    FakePlayer(ServerLevel level, String name) {
        this(level, new GameProfile(UUID.nameUUIDFromBytes(name.getBytes()), name));
    }

    private FakePlayer(ServerLevel level, GameProfile profile) {
        super(level.getServer(), level, profile, ClientInformation.createDefault());
        net.minecraft.network.Connection conn = new net.minecraft.network.Connection(PacketFlow.SERVERBOUND);
        channel = new EmbeddedChannel(conn);
        connection = new ServerGamePacketListenerImpl(level.getServer(), conn, this, CommonListenerCookie.createInitial(profile, false));
        connection.handleAcceptPlayerLoad(new net.minecraft.network.protocol.game.ServerboundPlayerLoadedPacket());
    }

    void tickUse(int n) {
        for (int i = 0; i < n && isUsingItem(); i++) updateUsingItem(getUseItem());
    }

    List<Packet<?>> sent() {
        List<Packet<?>> out = new ArrayList<>();
        Object o;
        while ((o = channel.readOutbound()) != null) {
            if (o instanceof Packet<?> p) out.add(p);
        }
        return out;
    }
}
