package net.minecraft.network.protocol.game;

import java.util.Optional;
import net.minecraft.network.chat.Component;
import net.minecraft.network.chat.numbers.NumberFormat;
import net.minecraft.network.protocol.Packet;

public record ClientboundSetScorePacket(String owner, String objectiveName, int score, Optional<Component> display,
                                        Optional<NumberFormat> numberFormat) implements Packet<ClientGamePacketListener> {
    public ClientboundSetScorePacket {
        if (display == null) display = Optional.empty();
        if (numberFormat == null) numberFormat = Optional.empty();
    }
}
