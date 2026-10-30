package net.minecraft.world.item.component;

import com.mojang.authlib.GameProfile;
import java.util.Optional;
import java.util.UUID;

/** A player profile that is either complete or known only by name or id. */
public final class ResolvableProfile {
    private final GameProfile profile;
    private final String name;
    private final UUID id;

    private ResolvableProfile(GameProfile profile, String name, UUID id) {
        this.profile = profile;
        this.name = name;
        this.id = id;
    }

    public static ResolvableProfile createResolved(GameProfile profile) {
        return new ResolvableProfile(profile, profile.name(), profile.id());
    }

    public static ResolvableProfile createUnresolved(String name) { return new ResolvableProfile(null, name, null); }

    public static ResolvableProfile createUnresolved(UUID id) { return new ResolvableProfile(null, null, id); }

    public Optional<GameProfile> profile() { return Optional.ofNullable(profile); }
    public Optional<String> name() { return Optional.ofNullable(name); }
    public Optional<UUID> id() { return Optional.ofNullable(id); }
    public boolean isResolved() { return profile != null; }
}
