package com.example.network

import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.POST
import retrofit2.http.Query

/**
 * Retrofit REST interface definition for the remote VPN Backend Server.
 * All operations (server list, connect dispatch, disconnect, telemetry, settings)
 * execute on the server side.
 */
interface VpnBackendApi {

  @GET("api/v1/health")
  suspend fun checkHealth(): Response<BackendHealthDto>

  @GET("api/v1/servers")
  suspend fun getServers(): Response<List<ServerNodeDto>>

  @POST("api/v1/connect")
  suspend fun connect(
    @Body request: ConnectRequestDto
  ): Response<ConnectResponseDto>

  @POST("api/v1/disconnect")
  suspend fun disconnect(
    @Body request: DisconnectRequestDto
  ): Response<DisconnectResponseDto>

  @GET("api/v1/telemetry")
  suspend fun getTelemetry(
    @Query("sessionId") sessionId: String?
  ): Response<ServerTelemetryDto>

  @POST("api/v1/settings")
  suspend fun syncSettings(
    @Body settings: UpdateSettingsRequestDto
  ): Response<Map<String, Boolean>>
}
